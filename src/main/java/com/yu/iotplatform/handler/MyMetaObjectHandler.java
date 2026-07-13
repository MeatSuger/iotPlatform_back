package com.yu.iotplatform.handler;

import com.baomidou.mybatisplus.core.handlers.MetaObjectHandler;
import lombok.extern.slf4j.Slf4j;
import org.apache.ibatis.reflection.MetaObject;
import org.springframework.stereotype.Component;

import java.time.OffsetDateTime;

@Slf4j
@Component
public class MyMetaObjectHandler implements MetaObjectHandler {
	@Override
	public void insertFill(MetaObject metaObject) {
		log.info("开始插入填充...");
		this.strictInsertFill(metaObject, "createdAt", OffsetDateTime.class, OffsetDateTime.now());

	}

	@Override
	public void updateFill(MetaObject metaObject) {
		log.info("开始更新填充...");
		this.strictUpdateFill(metaObject, "updatedAt", OffsetDateTime.class, OffsetDateTime.now());
	}
}
