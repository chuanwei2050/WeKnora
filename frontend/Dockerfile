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

# 构建应用和可独立发布的悬浮聊天 SDK
RUN npm run build && npm run build:widget

# 生产阶段
FROM nginx:stable-alpine AS production-stage

# 复制构建产物到nginx服务目录
COPY --from=build-stage /app/dist /usr/share/nginx/html
COPY --from=build-stage /app/dist-widget /usr/share/nginx/html/widget

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
