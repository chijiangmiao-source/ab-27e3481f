# ---- 构建阶段 ----
FROM node:20-alpine AS build
WORKDIR /src

COPY package.json package-lock.json* ./
RUN npm install

COPY . .
RUN npm run build

# ---- 运行阶段 ----
FROM nginx:1.27-alpine
RUN apk add --no-cache wget

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
  CMD wget -qO- http://127.0.0.1:80/healthz || exit 1

EXPOSE 80
