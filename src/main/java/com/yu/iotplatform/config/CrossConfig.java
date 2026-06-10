package com.yu.iotplatform.config;

import cn.dev33.satoken.context.SaHolder;
import cn.dev33.satoken.filter.SaServletFilter;
import cn.dev33.satoken.router.SaRouter;
import com.yu.iotplatform.common.ApiResponse;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
public class CrossConfig {
    /**
     * 跨域处理策略
     */
    @Bean
    public SaServletFilter getSaServletFilter() {
        return new SaServletFilter()
                // 1. 拦截所有路径
                .addInclude("/**")
                // 2. 放行路径（根据需要添加）
                .addExclude("/favicon.ico", "/v3/api-docs/**", "/swagger-ui/**")

                // 3. 前置处理：替代已废弃的 SaCorsHandleFunction 处理跨域
                .setBeforeAuth(obj -> {
                    // 自定义跨域响应头
//                    SaHolder.getResponse().setHeader("Access-Control-Allow-Origin", "http://localhost:8181")
                    SaHolder.getResponse().setHeader("Access-Control-Allow-Origin", "https://iot.meatsuger.top")
                            .setHeader("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
                            .setHeader("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Accept, Origin")
                            .setHeader("Access-Control-Allow-Credentials", "true")
                            .setHeader("Access-Control-Max-Age", "3600");

                    // 如果是 OPTIONS 预检请求，直接停止并返回
                    if ("OPTIONS".equalsIgnoreCase(SaHolder.getRequest().getMethod())) {
                        SaRouter.stop();
                    }
                })

                // 4. 认证函数 (v1.40.0 强化了路由匹配的性能)
                .setAuth(obj -> {
                    // 在此处添加全局拦截逻辑
                    // SaRouter.match("/admin/**", r -> StpUtil.checkPermission("admin"));
                })

                // 5. 异常处理
                .setError(e ->
                        ApiResponse.fail(400,"系统安全验证失败: " + e.getMessage()));
    }
}
