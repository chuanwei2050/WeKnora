# 构建阶段
FROM node:24-alpine AS build-stage

WORKDIR /app

# 设置环境变量，忽略类型检查错误
ENV NODE_OPTIONS="--max-old-space-size=4096"
ENV VITE_IS_DOCKER=true

# 复制依赖文件
COPY package*.json ./
COPY packages/xlsx-0.20.2.tgz ./packages/xlsx-0.20.2.tgz

# 使用仓库已提交的 package-lock.json 进行可复现安装
RUN npm ci

# 复制项目文件
COPY . .

# 构建：独立站 base=/；同域嵌入 base=/knowledge/embed/（避免与宿主 /assets 冲突）
RUN npm run build && npm run build:widget \
  && mkdir -p /tmp/weknora-out/widget /tmp/weknora-out/knowledge/embed \
  && cp -a dist/. /tmp/weknora-out/ \
  && cp -a dist-widget/. /tmp/weknora-out/widget/ \
  && VITE_APP_BASE_PATH=/knowledge/embed/ npm run build \
  && cp -a dist/. /tmp/weknora-out/knowledge/embed/ \
  && rm -rf dist \
  && mv /tmp/weknora-out dist

# 生产阶段
FROM nginx:stable-alpine AS production-stage

# 复制构建产物到nginx服务目录
COPY --from=build-stage /app/dist /usr/share/nginx/html
# 复制nginx配置模板文件
COPY nginx.conf /etc/nginx/templates/default.conf.template

# 复制启动脚本
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# 设置默认环境变量（MB）
ENV MAX_FILE_SIZE_MB=50

# 暴露端口
EXPOSE 80

ENTRYPOINT ["/docker-entrypoint.sh"]
