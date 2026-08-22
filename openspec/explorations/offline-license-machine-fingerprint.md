# 离线许可证与机器指纹设计草案

> 状态：探索中，当前代码尚未实现本文所述许可证与机器指纹机制，不应作为现有部署能力使用。

## 1. 目标和适用范围

本方案用于 Linux Docker 私有化交付，目标是防止客户将已交付的业务镜像、Compose 文件和数据直接复制到另一台普通服务器运行。

方案由三部分组成：

1. 使用宿主机 `/etc/machine-id` 和 DMI `product_uuid` 生成机器码。
2. 使用供应商持有的 Ed25519 私钥签发离线许可证。
3. 业务二进制内部强制验签，验证通过后才启动服务。

本方案能防止普通的二次复制部署，不能从理论上阻止拥有 root 权限且有能力修改机器标识或补丁二进制的攻击者。高对抗场景应将机器身份升级为 TPM 2.0 挑战签名或 USB 加密狗。

## 2. 完整流程

```text
你方首次准备
  ├─ 生成 Ed25519 公钥和私钥
  ├─ 公钥编译进业务二进制
  └─ 私钥仅保存在你方签发环境

客户首次部署
  ├─ 客户在目标服务器运行 machine-code 命令
  ├─ 客户将机器码发给你方
  ├─ 你方使用私钥签发 license.json
  ├─ 客户将 license.json 放入部署目录
  └─ 客户运行 docker compose up -d

应用每次启动
  ├─ 验证 Ed25519 签名
  ├─ 验证 product_id
  ├─ 重新计算当前服务器机器码
  ├─ 比较许可证机器码
  ├─ 验证有效期
  └─ 全部通过后启动业务
```

## 3. 部署角色和交付物

### 3.1 你方保留

```text
license-admin             许可证签发工具
private.key               Ed25519 私钥
许可证台账             客户、产品、机器码、到期日期
```

`private.key` 不得进入业务 Git 仓库、CI 日志、Docker 构建上下文、Docker 镜像或客户交付包。

### 3.2 交付客户

```text
delivery/
├── images/
│   └── weknora-core-images-1.0.0.tar
├── docker-compose.yml
├── license/
│   └── license.json
└── README.txt
```

公钥已编译进业务二进制，不需要单独交付。公钥不是秘密，但不能从客户可编辑的配置或环境变量中加载，否则客户可替换为自己的公钥。

## 4. 项目代码改造

### 4.1 新增目录

在 WeKnora 项目中新增：

```text
internal/license/
├── license.go
└── public.key
```

`public.key` 内容是 Base64 编码的 Ed25519 公钥。

### 4.2 新增 `internal/license/license.go`

```go
package license

import (
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	ProductID       = "weknora"
	LicensePath     = "/run/weknora-license/license.json"
	MachineIDPath   = "/host/etc/machine-id"
	ProductUUIDPath = "/host/sys/class/dmi/id/product_uuid"
)

//go:embed public.key
var publicKeyBase64 string

// Required 由构建参数写入二进制。开发构建默认不要求许可证，
// 客户交付镜像必须在构建时将其固化为 true。
var Required = "false"

func IsRequired() (bool, error) {
	switch Required {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("无效的编译期授权开关: %q", Required)
	}
}

type Claims struct {
	LicenseID   string    `json:"license_id"`
	CustomerID  string    `json:"customer_id"`
	ProductID   string    `json:"product_id"`
	MachineCode string    `json:"machine_code"`
	IssuedAt    time.Time `json:"issued_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type File struct {
	SchemaVersion int    `json:"schema_version"`
	Payload       string `json:"payload"`
	Signature     string `json:"signature"`
}

func MachineCode() (string, error) {
	machineID, err := readRequiredID(MachineIDPath)
	if err != nil {
		return "", fmt.Errorf("读取宿主机 machine-id: %w", err)
	}

	productUUID, err := readRequiredID(ProductUUIDPath)
	if err != nil {
		return "", fmt.Errorf("读取宿主机 product UUID: %w", err)
	}

	source := strings.Join([]string{
		"weknora-license-v1",
		ProductID,
		machineID,
		productUUID,
	}, "\n")

	sum := sha256.Sum256([]byte(source))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func Verify() (*Claims, error) {
	raw, err := os.ReadFile(LicensePath)
	if err != nil {
		return nil, fmt.Errorf("读取许可证失败: %w", err)
	}

	var file File
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("许可证格式错误: %w", err)
	}
	if file.SchemaVersion != 1 {
		return nil, fmt.Errorf("不支持的许可证版本: %d", file.SchemaVersion)
	}

	payload, err := base64.RawURLEncoding.DecodeString(file.Payload)
	if err != nil {
		return nil, fmt.Errorf("许可证 payload 编码错误")
	}
	signature, err := base64.RawURLEncoding.DecodeString(file.Signature)
	if err != nil {
		return nil, fmt.Errorf("许可证签名编码错误")
	}

	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyBase64))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("程序内置授权公钥无效")
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
		return nil, fmt.Errorf("许可证签名无效")
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("许可证内容错误: %w", err)
	}
	if claims.LicenseID == "" || claims.CustomerID == "" || claims.MachineCode == "" {
		return nil, fmt.Errorf("许可证缺少必填字段")
	}
	if claims.ProductID != ProductID {
		return nil, fmt.Errorf("许可证不属于当前产品")
	}

	currentMachineCode, err := MachineCode()
	if err != nil {
		return nil, err
	}
	if claims.MachineCode != currentMachineCode {
		return nil, fmt.Errorf("许可证与当前服务器不匹配")
	}
	if claims.ExpiresAt.IsZero() {
		return nil, fmt.Errorf("许可证缺少到期时间")
	}
	if time.Now().UTC().After(claims.ExpiresAt) {
		return nil, fmt.Errorf("许可证已于 %s 过期", claims.ExpiresAt.Format("2006-01-02"))
	}

	return &claims, nil
}

func readRequiredID(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.ToLower(strings.TrimSpace(string(raw)))
	if value == "" {
		return "", fmt.Errorf("标识文件为空")
	}
	return value, nil
}
```

### 4.3 修改 `cmd/server/main.go`

增加导入：

```go
"github.com/Tencent/WeKnora/internal/license"
```

在当前 `main()` 函数最前面增加：

```go
func main() {
	// 首次部署时只输出机器码，不启动业务，也不要求许可证。
	if len(os.Args) == 2 && os.Args[1] == "machine-code" {
		code, err := license.MachineCode()
		if err != nil {
			fmt.Fprintf(os.Stderr, "生成机器码失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(code)
		return
	}

	// 只有客户交付构建强制验证许可证。
	// 该开关编译在二进制中，不能由客户通过环境变量修改。
	required, err := license.IsRequired()
	if err != nil {
		fmt.Fprintf(os.Stderr, "授权构建配置错误: %v\n", err)
		os.Exit(78)
	}
	if required {
		claims, err := license.Verify()
		if err != nil {
			fmt.Fprintf(os.Stderr, "授权校验失败: %v\n", err)
			os.Exit(78)
		}
		fmt.Printf(
			"授权校验成功，客户=%s，许可证=%s，到期=%s\n",
			claims.CustomerID,
			claims.LicenseID,
			claims.ExpiresAt.Format("2006-01-02"),
		)
	}

	// 从这里开始保留原有启动代码。
	// ...
}
```

客户交付构建中，授权校验必须在以下行为之前完成：

- 构建依赖注入容器。
- 运行数据库迁移。
- 监听 HTTP/gRPC 端口。
- 启动定时任务。
- 消费消息队列。

不要将授权开关做成环境变量，也不要只在 Shell 脚本或 Docker `entrypoint` 中校验，因为客户可修改环境变量或覆盖入口。

### 4.4 修改 `Makefile`

在 `build-prod` 的 Shell 变量中增加：

```make
LICENSE_REQUIRED=$${LICENSE_REQUIRED:-false}; \
```

并在现有 `LDFLAGS` 中追加：

```make
-X 'github.com/Tencent/WeKnora/internal/license.Required=$$LICENSE_REQUIRED'
```

保留 `build-prod` 中原有的版本、Edition 和 Protobuf 参数，不要用上面片段覆盖整个 `LDFLAGS`。普通本地构建默认为 `false`，客户交付构建必须为 `true`。

### 4.5 修改 `docker/Dockerfile.app`

在 builder 阶段、执行 `make build-prod` 之前增加：

```dockerfile
ARG LICENSE_REQUIRED_ARG=false
ENV LICENSE_REQUIRED=${LICENSE_REQUIRED_ARG}
```

在最终镜像中预先创建挂载目录：

```dockerfile
RUN mkdir -p \
    /run/weknora-license \
    /host/etc \
    /host/sys/class/dmi/id
```

公钥使用 `go:embed` 编译进 `WeKnora` 二进制，最终镜像不需要单独复制 `public.key`。

### 4.6 修改 `docker-compose.yml`

在 `app.volumes` 中增加：

```yaml
services:
  app:
    volumes:
      - ./license/license.json:/run/weknora-license/license.json:ro
      - /etc/machine-id:/host/etc/machine-id:ro
      - /sys/class/dmi/id/product_uuid:/host/sys/class/dmi/id/product_uuid:ro
```

这里必须挂载宿主机文件，不能读取容器自身的 machine-id。

## 5. 你方签发工具

签发工具应放在与客户交付项目分离的内部仓库。下面是一个完整的最小 Go 实现。

### 5.1 创建内部工具

```bash
mkdir license-admin
cd license-admin
go mod init company.internal/license-admin
```

创建 `main.go`：

```go
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

type claims struct {
	LicenseID   string    `json:"license_id"`
	CustomerID  string    `json:"customer_id"`
	ProductID   string    `json:"product_id"`
	MachineCode string    `json:"machine_code"`
	IssuedAt    time.Time `json:"issued_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type licenseFile struct {
	SchemaVersion int    `json:"schema_version"`
	Payload       string `json:"payload"`
	Signature     string `json:"signature"`
}

func main() {
	if len(os.Args) < 2 {
		fatalf("用法: license-admin <keygen|issue> [参数]")
	}

	switch os.Args[1] {
	case "keygen":
		runKeygen(os.Args[2:])
	case "issue":
		runIssue(os.Args[2:])
	default:
		fatalf("未知命令: %s", os.Args[1])
	}
}

func runKeygen(args []string) {
	flags := flag.NewFlagSet("keygen", flag.ExitOnError)
	privatePath := flags.String("private", "private.key", "私钥输出路径")
	publicPath := flags.String("public", "public.key", "公钥输出路径")
	_ = flags.Parse(args)
	if *privatePath == *publicPath {
		fatalf("公钥和私钥输出路径不能相同")
	}
	ensureAbsent(*privatePath)
	ensureAbsent(*publicPath)

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fatalf("生成密钥: %v", err)
	}

	privateText := base64.StdEncoding.EncodeToString(privateKey)
	publicText := base64.StdEncoding.EncodeToString(publicKey)
	if err := writeExclusive(*privatePath, []byte(privateText+"\n"), 0600); err != nil {
		fatalf("写入私钥: %v", err)
	}
	if err := writeExclusive(*publicPath, []byte(publicText+"\n"), 0644); err != nil {
		_ = os.Remove(*privatePath)
		fatalf("写入公钥: %v", err)
	}

	fmt.Printf("已生成私钥: %s\n", *privatePath)
	fmt.Printf("已生成公钥: %s\n", *publicPath)
}

func runIssue(args []string) {
	flags := flag.NewFlagSet("issue", flag.ExitOnError)
	privatePath := flags.String("private", "private.key", "私钥路径")
	licenseID := flags.String("license-id", "", "许可证编号")
	customerID := flags.String("customer", "", "客户编号")
	productID := flags.String("product", "", "产品编号")
	machineCode := flags.String("machine-code", "", "客户机器码")
	expires := flags.String("expires", "", "到期日期，格式 YYYY-MM-DD")
	output := flags.String("output", "license.json", "许可证输出路径")
	_ = flags.Parse(args)

	if *licenseID == "" || *customerID == "" || *productID == "" ||
		*machineCode == "" || *expires == "" {
		fatalf("license-id、customer、product、machine-code 和 expires 均为必填")
	}

	expiryDate, err := time.Parse("2006-01-02", *expires)
	if err != nil {
		fatalf("到期日期格式错误: %v", err)
	}
	expiresAt := time.Date(
		expiryDate.Year(), expiryDate.Month(), expiryDate.Day(),
		23, 59, 59, 0, time.UTC,
	)

	privateKey := readPrivateKey(*privatePath)
	now := time.Now().UTC()
	payload, err := json.Marshal(claims{
		LicenseID:   *licenseID,
		CustomerID:  *customerID,
		ProductID:   *productID,
		MachineCode: *machineCode,
		IssuedAt:    now,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		fatalf("生成许可证内容: %v", err)
	}

	signature := ed25519.Sign(privateKey, payload)
	result, err := json.MarshalIndent(licenseFile{
		SchemaVersion: 1,
		Payload:       base64.RawURLEncoding.EncodeToString(payload),
		Signature:     base64.RawURLEncoding.EncodeToString(signature),
	}, "", "  ")
	if err != nil {
		fatalf("生成许可证文件: %v", err)
	}
	result = append(result, '\n')

	if err := os.WriteFile(*output, result, 0644); err != nil {
		fatalf("写入许可证: %v", err)
	}
	fmt.Printf("已签发许可证: %s\n", *output)
}

func readPrivateKey(path string) ed25519.PrivateKey {
	raw, err := os.ReadFile(path)
	if err != nil {
		fatalf("读取私钥: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		fatalf("私钥格式无效")
	}
	return ed25519.PrivateKey(decoded)
}

func ensureAbsent(path string) {
	_, err := os.Stat(path)
	if err == nil {
		fatalf("拒绝覆盖已有密钥文件: %s", path)
	}
	if !os.IsNotExist(err) {
		fatalf("检查密钥路径 %s: %v", path, err)
	}
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if written != len(data) {
		_ = os.Remove(path)
		return fmt.Errorf("密钥文件写入不完整")
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
```

### 5.2 编译签发工具

```bash
go build -trimpath -o license-admin .
```

Windows 会生成 `license-admin.exe`。

### 5.3 生成密钥

只在你方安全环境执行一次：

```bash
./license-admin keygen \
  --private private.key \
  --public public.key
```

重复执行相同命令必须报错并保持原密钥不变：

```text
拒绝覆盖已有密钥文件: private.key
```

将 `public.key` 复制到 WeKnora：

```text
internal/license/public.key
```

将 `private.key` 移入受控的秘密存储。在签发工具目录的 `.gitignore` 中至少加入：

```gitignore
private.key
```

## 6. 你方构建和交付镜像

以下命令产生一套不依赖客户公网镜像仓库的默认核心交付包。统一使用一个不变的交付版本标签：

```bash
export WEKNORA_VERSION=licensed-1.0.0
```

### 6.1 构建受保护的 App 镜像

`LICENSE_REQUIRED_ARG=true` 是客户交付构建的必填参数：

```bash
docker build \
  --build-arg LICENSE_REQUIRED_ARG=true \
  -f docker/Dockerfile.app \
  -t wechatopenai/weknora-app:${WEKNORA_VERSION} .
```

不得将客户交付镜像构建为 `LICENSE_REQUIRED_ARG=false`。此参数只在构建时生效，客户无法通过运行时环境变量关闭。

构建后立即证明镜像在无许可证时是因授权校验而拒绝启动：

```bash
output="$(docker run --rm \
  --entrypoint ./WeKnora \
  wechatopenai/weknora-app:${WEKNORA_VERSION} 2>&1 || true)"
printf '%s\n' "$output" | grep -F '授权校验失败: 读取许可证失败'
```

`grep` 没有匹配时必须终止交付，说明授权开关没有正确编译进镜像。

### 6.2 构建其他 WeKnora 核心镜像

```bash
docker build \
  -t wechatopenai/weknora-ui:${WEKNORA_VERSION} \
  ./frontend

docker build \
  -f docker/Dockerfile.docreader \
  -t wechatopenai/weknora-docreader:${WEKNORA_VERSION} .
```

在你方有网环境预先获取 Compose 默认启动的基础镜像：

```bash
docker pull paradedb/paradedb:v0.22.2-pg17
docker pull redis:7.0-alpine
```

### 6.3 导出完整的默认核心镜像包

```bash
docker save \
  -o weknora-core-images-1.0.0.tar \
  wechatopenai/weknora-app:${WEKNORA_VERSION} \
  wechatopenai/weknora-ui:${WEKNORA_VERSION} \
  wechatopenai/weknora-docreader:${WEKNORA_VERSION} \
  paradedb/paradedb:v0.22.2-pg17 \
  redis:7.0-alpine
```

上面包含默认 Compose 启动所需的 App、Frontend、Docreader、PostgreSQL 和 Redis。如果启用 `full`、`minio`、`qdrant`、`neo4j` 等可选 profile，必须将对应镜像一并加入交付包。

### 6.4 记录交付镜像 ID

```bash
for image in \
  wechatopenai/weknora-app:${WEKNORA_VERSION} \
  wechatopenai/weknora-ui:${WEKNORA_VERSION} \
  wechatopenai/weknora-docreader:${WEKNORA_VERSION} \
  paradedb/paradedb:v0.22.2-pg17 \
  redis:7.0-alpine
do
  docker image inspect "$image" --format '{{.Id}} {{join .RepoTags ","}}'
done
```

## 7. 客户首次部署

以下是可直接交给客户的操作说明。

### 7.1 导入镜像

```bash
docker load -i weknora-core-images-1.0.0.tar
```

在部署目录的 `.env` 中设置与交付镜像一致的版本：

```dotenv
WEKNORA_VERSION=licensed-1.0.0
```

确认默认核心镜像均已导入：

```bash
docker image inspect \
  wechatopenai/weknora-app:licensed-1.0.0 \
  wechatopenai/weknora-ui:licensed-1.0.0 \
  wechatopenai/weknora-docreader:licensed-1.0.0 \
  paradedb/paradedb:v0.22.2-pg17 \
  redis:7.0-alpine >/dev/null
```

### 7.2 在目标服务器生成机器码

必须在实际生产服务器上执行：

```bash
docker run --rm \
  --entrypoint ./WeKnora \
  -v /etc/machine-id:/host/etc/machine-id:ro \
  -v /sys/class/dmi/id/product_uuid:/host/sys/class/dmi/id/product_uuid:ro \
  wechatopenai/weknora-app:licensed-1.0.0 \
  machine-code
```

预期输出：

```text
sha256:68c178213e74f873...
```

客户将以下信息发给你方：

```text
客户编号：CUSTOMER-001
产品编号：weknora
部署环境：生产
机器码：sha256:68c178213e74f873...
```

客户不需要发送原始 `/etc/machine-id` 或 `product_uuid`。

### 7.3 你方签发许可证

在你方签发环境执行：

```bash
./license-admin issue \
  --private private.key \
  --license-id LIC-2026-0001 \
  --customer CUSTOMER-001 \
  --product weknora \
  --machine-code 'sha256:68c178213e74f873...' \
  --expires 2027-08-18 \
  --output license.json
```

将生成的 `license.json` 发给客户。

### 7.4 客户安装许可证

客户在 Compose 部署目录执行：

```bash
mkdir -p license
cp license.json license/license.json
chmod 0444 license/license.json
```

确认文件结构：

```text
deployment/
├── docker-compose.yml
├── config/
└── license/
    └── license.json
```

### 7.5 客户启动系统

```bash
docker compose up -d
docker compose logs app
```

正常日志：

```text
授权校验成功，客户=CUSTOMER-001，许可证=LIC-2026-0001，到期=2027-08-18
```

许可证不匹配时：

```text
授权校验失败: 许可证与当前服务器不匹配
```

此时业务进程以退出码 `78` 退出，不启动 HTTP 服务。

## 8. 客户日常运行

客户正常启停方式与原来一致：

```bash
docker compose up -d
docker compose stop
docker compose restart app
docker compose logs -f app
```

每次容器重启都会重新验证许可证。在原服务器上重建容器不会改变宿主机机器码。

## 9. 续期流程

客户服务器没有变更时，沿用原机器码签发新许可证：

```bash
./license-admin issue \
  --private private.key \
  --license-id LIC-2027-0001 \
  --customer CUSTOMER-001 \
  --product weknora \
  --machine-code 'sha256:68c178213e74f873...' \
  --expires 2028-08-18 \
  --output license.json
```

客户替换后重启：

```bash
chmod u+w license/license.json
cp license.json license/license.json
chmod 0444 license/license.json
docker compose restart app
docker compose logs app
```

建议许可证有效期为 6 至 12 个月，并在到期前 30 天开始续期。

## 10. 换机流程

服务器更换后机器码会改变，客户需要：

1. 在新服务器重新运行 `machine-code` 命令。
2. 将旧机器码、新机器码和换机原因发给你方。
3. 你方将旧许可证在台账中标记为“已换机”。
4. 你方对新机器码签发新许可证。
5. 客户书面确认旧服务器已停用。

纯离线环境无法在线撤销已交付的旧许可证，因此旧环境下线仍需要合同约束和交付记录。

## 11. 验收清单

上线前必须完成以下验收：

| 场景 | 预期结果 |
|---|---|
| 重复执行 `license-admin keygen` | 拒绝覆盖，原密钥哈希不变 |
| 普通开发构建无许可证 | 不执行客户授权校验 |
| 客户交付构建无许可证 | 拒绝启动 |
| 无许可证执行 `machine-code` | 正常输出机器码后退出 |
| 导入核心镜像包 | App、Frontend、Docreader、PostgreSQL 和 Redis 全部存在 |
| 正确许可证 | 正常启动 |
| 修改 `customer_id` | Ed25519 签名验证失败 |
| 修改 `expires_at` | Ed25519 签名验证失败 |
| 使用其他产品许可证 | 拒绝启动 |
| 许可证过期 | 拒绝启动 |
| 在原服务器重建容器 | 正常启动 |
| 复制镜像和许可证到另一台服务器 | 机器码不匹配，拒绝启动 |

正确性标准：

```text
客户交付构建 + 原服务器 + 未篡改且未过期的许可证 = 启动成功
客户交付构建的任意其他组合 = 启动失败
```

## 12. 多项目复用规则

每个项目使用唯一且永久不变的 `product_id`：

```text
weknora
document-platform
crm-server
model-service
```

所有项目复用：

- 同一份许可证 JSON 协议。
- 同一组 Ed25519 签发密钥，或按产品线分组的密钥。
- 同一个 `license-admin` 签发工具。
- 相同的机器指纹算法和协议版本。

每个业务项目只需定义自己的 `product_id`、嵌入公钥并在启动入口调用验证函数。

不要在不同项目中改变机器码拼接顺序、大小写处理或换行符，否则会生成不兼容的机器码。

## 13. 安全边界

本方案能阻止：

- 直接复制 Docker 镜像到另一台服务器。
- 复制 Compose、许可证和数据卷后启动。
- 修改许可证客户、产品、机器码或有效期。
- 使用自行构造但未经你方签名的许可证。

本方案不能彻底阻止：

- 客户使用 root 权限伪造挂载的 `machine-id` 和 `product_uuid`。
- 完整克隆并保留所有标识的虚拟机。
- 专业人员补丁业务二进制跳过授权检查。
- 在同一台已授权服务器上启动多个容器实例。
- 在完全离线环境中实时撤销已签发的许可证。

建议同时采用：

- 只交付最终运行镜像，不交付源码和构建阶段。
- 构建时删除调试信息和符号，适度混淆授权检查所在包。
- 每个客户使用独立的 `license_id` 和交付记录。
- 合同明确限定服务器、生产/测试环境、产品和实例数。
- 高价值交付升级为 TPM 2.0 或 USB 加密狗。

## 14. 故障排查

### 14.1 无法读取 machine-id

检查：

```bash
test -s /etc/machine-id && echo OK
```

确认 Compose 已挂载：

```yaml
- /etc/machine-id:/host/etc/machine-id:ro
```

### 14.2 无法读取 product UUID

检查：

```bash
test -s /sys/class/dmi/id/product_uuid && echo OK
```

如果客户环境确实不提供 DMI UUID，不要在客户现场随意修改算法。应在你方统一评估备选标识，升级协议版本并重新发布镜像。

### 14.3 容器不断重启

Compose 使用 `restart: unless-stopped` 时，授权失败会导致容器重复重启。先查看日志：

```bash
docker compose logs --tail=100 app
```

需要停止重试时：

```bash
docker compose stop app
```

### 14.4 原服务器机器码突然改变

常见原因：

- 宿主机重装系统。
- 虚拟机 UUID 被重置。
- 实际运行在新服务器。
- Compose 挂载路径被修改。

处理方式是按换机流程重新生成机器码和签发许可证，不要向客户提供跳过校验的环境变量或后门。

## 15. 上线前最终检查

- [ ] 私钥不在业务仓库。
- [ ] 私钥不在 Docker 构建上下文和镜像中。
- [ ] 重复执行 `keygen` 会拒绝覆盖已有公私钥。
- [ ] 公钥已通过 `go:embed` 编译进二进制。
- [ ] 普通开发构建不要求客户许可证。
- [ ] 客户交付镜像使用 `LICENSE_REQUIRED_ARG=true` 构建。
- [ ] 客户交付镜像在无许可证时拒绝启动。
- [ ] `machine-code` 命令不需要许可证，但不启动业务。
- [ ] 正常启动在任何业务初始化前验证许可证。
- [ ] 宿主机 machine-id 和 product UUID 只读挂载。
- [ ] 默认核心交付包包含 App、Frontend、Docreader、PostgreSQL 和 Redis 镜像。
- [ ] 篡改许可证后启动失败。
- [ ] 复制到另一台服务器后启动失败。
- [ ] 许可证续期和换机流程已进行演练。
- [ ] 合同和交付台账已记录授权范围。
