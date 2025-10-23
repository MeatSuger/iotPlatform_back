package com.yu.iotplatform.handler;


import cn.dev33.satoken.context.SaHolder;
import cn.dev33.satoken.exception.NotLoginException;
import cn.dev33.satoken.exception.NotPermissionException;
import com.yu.iotplatform.common.ApiResponse;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;


/**
 * 统一异常拦截类
 */
@RestControllerAdvice
public class GlobalExceptionHandler {


    /**
     * 没有权限拦截类
     * @param e
     * @return
     */
    @ExceptionHandler(NotPermissionException.class)
    public ApiResponse<String> notPermissionExceptionHandler(NotPermissionException e) {
        SaHolder.getResponse().setStatus(HttpStatus.FORBIDDEN.value());
        return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(),  e.getMessage());
    }
    @ExceptionHandler(NotLoginException.class)
    public ApiResponse<String> notLoginExceptionHandler(NotLoginException e) {
        SaHolder.getResponse().setStatus(HttpStatus.UNAUTHORIZED.value());
        return ApiResponse.fail(HttpStatus.UNAUTHORIZED.value(),  e.getMessage());
    }
}
