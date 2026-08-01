FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates curl && rm -rf /var/lib/apt/lists/*
WORKDIR /app
# 二进制通过 docker-compose volumes 挂载，无需 COPY
# 如需不打挂载直接构建镜像，取消下一行注释：
# COPY ../../build/iot-platform /app/iot-platform
CMD ["./iot-platform"]
