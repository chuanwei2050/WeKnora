#!/bin/sh

# 生成运行时配置文件，注入环境变量到前端
cat > /usr/share/nginx/html/config.js << EOF
window.__RUNTIME_CONFIG__ = {
  MAX_FILE_SIZE_MB: ${MAX_FILE_SIZE_MB:-2047}
};
EOF

# 处理 nginx 配置
export MAX_FILE_SIZE=${MAX_FILE_SIZE_MB}M
export APP_HOST=${APP_HOST:-app}
export APP_PORT=${APP_PORT:-8080}
export APP_SCHEME=${APP_SCHEME:-http}
FRAME_ANCESTORS=${FRAME_ANCESTORS:-"'self'"}
# Docker Compose .env parsing may strip quotes or truncate at spaces (e.g. FRAME_ANCESTORS=self).
case "$FRAME_ANCESTORS" in
  self) FRAME_ANCESTORS="'self'" ;;
esac
export FRAME_ANCESTORS
envsubst '${MAX_FILE_SIZE} ${APP_HOST} ${APP_PORT} ${APP_SCHEME} ${FRAME_ANCESTORS}' < /etc/nginx/templates/default.conf.template > /etc/nginx/conf.d/default.conf

# 启动 nginx
exec nginx -g 'daemon off;'
