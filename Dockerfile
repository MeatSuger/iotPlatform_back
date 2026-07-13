FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY build/iot-platform /app/iot-platform
WORKDIR /app
CMD ["./iot-platform"]
