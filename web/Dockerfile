# Build context is the repo root so deploy/nginx/default.conf is reachable:
#   docker build -f web/Dockerfile .
FROM node:24-alpine AS build
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown
ENV VITE_BUILD_VERSION=$VERSION VITE_BUILD_COMMIT=$COMMIT VITE_BUILD_TIME=$BUILT_AT
RUN npm run build

FROM nginx:1.31-alpine
COPY --from=build /app/web/dist /usr/share/nginx/html
COPY deploy/nginx/default.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
