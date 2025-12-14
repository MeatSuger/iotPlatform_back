# Back (Spring Boot)

Spring Boot 后端服务，使用 Maven 构建。

项目结构

- `pom.xml`：依赖与构建配置
- `src/main/java`：业务源码
- `src/main/resources`：配置与资源（`application.yml`, `application-dev.yml`, `application-prod.yml`, `banner.txt`）
- `src/test/java`：测试代码

环境要求

- Java 17（或项目 pom 指定版本）
- Maven 3.8+
- 可选外部服务：数据库、Redis、MQTT、InfluxDB 等（按你的 `application*.yml` 配置）

本地运行

```powershell
cd .\back
mvn -v
mvn clean install -DskipTests
mvn spring-boot:run
```

切换环境

- 方式一：启动参数 `--spring.profiles.active=dev|prod`
- 方式二：环境变量 `SPRING_PROFILES_ACTIVE=dev|prod`

打包与运行 Jar

```powershell
cd .\back
mvn clean package -DskipTests
java -jar target\iotPlatform-0.0.1-SNAPSHOT.jar.original --spring.profiles.active=prod
```

配置说明

- 默认配置文件在 `src/main/resources/`
- 若使用外部中间件，请在 `application.yml` 中填充连接信息（例如 `spring.data.redis`, `spring.datasource`, `mqtt`, `influx` 等键）

测试

```powershell
mvn test
```

常见问题

- 端口占用：修改 `application.yml` 中的 `server.port`
- CORS：若前端跨域报错，配置 `CorsFilter` 或 Spring Security 的跨域策略
- 日志：查看 `logs` 或控制台输出，确保 banner 与 profile 正确加载

部署建议

- 生产环境请使用 `prod` 配置，并在系统级环境变量或 `application-prod.yml` 中保密敏感信息
- 推荐使用容器化或系统服务管理（如 NSSM、systemd）来守护进程
