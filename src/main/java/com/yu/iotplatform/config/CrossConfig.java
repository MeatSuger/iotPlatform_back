package com.yu.iotplatform.config;

import cn.dev33.satoken.fun.strategy.SaCorsHandleFunction;
import cn.dev33.satoken.router.SaHttpMethod;
import cn.dev33.satoken.router.SaRouter;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
public class CrossConfig {
    /**
     * 跨域处理策略
     */
    @Bean
    public SaCorsHandleFunction saCorsHandle() {
        return (saRequest, saResponse, saStorage) -> {
//            saResponse.setHeader("Access-Control-Allow-Origin", "http://localhost:5173")
            saResponse.setHeader("Access-Control-Allow-Origin", "https://www.meatsuger.top")
                    .setHeader("Access-Control-Allow-Methods", "POST, GET, OPTIONS, DELETE, PUT")
                    .setHeader("Access-Control-Max-Age", "3600")
                    .setHeader("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Accept, Origin, Cache-Control, X-CSRF-Token")
                    .setHeader("Access-Control-Allow-Credentials", "true");
            SaRouter.match(SaHttpMethod.OPTIONS)
                    .free(saRouterStaff ->
                            System.out.println("--------OPTIONS预检请求，不做处理"))
                    .back();
        };
    }
}
