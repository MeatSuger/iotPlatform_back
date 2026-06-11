package com.yu.iotplatform.config;


import cn.dev33.satoken.interceptor.SaInterceptor;
import cn.dev33.satoken.router.SaRouter;
import cn.dev33.satoken.stp.StpUtil;
import lombok.extern.slf4j.Slf4j;
import org.springframework.context.annotation.Configuration;
import org.springframework.web.servlet.config.annotation.InterceptorRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;

@Slf4j
@Configuration
public class SaTokenConfig implements WebMvcConfigurer {

	// 注册 Sa-Token 拦截器，打开注解式鉴权功能
	@Override
	public void addInterceptors(InterceptorRegistry registry) {
		registry.addInterceptor(new SaInterceptor(handle -> SaRouter.match("/**")
				// 排除业务接口
				.notMatch("/user/login",
						"/user/register",
						"/device/register",
						"/device/{deviceId}/login",
						"/data/*/Data",
						"/data/*/ping",
						"/data/*/heartbeat")
				// 排除 Swagger 3.0 相关全部资源 (注意路径开头的 / 和末尾的 /**)
				.notMatch(
						"/swagger-ui.html",
						"/swagger-ui/**",
						"/v3/api-docs/**",
						"/swagger-resources/**",
						"/webjars/**"
				)
				.check(r -> StpUtil.checkLogin()))).addPathPatterns("/**");
	}

}
