package com.yu.iotplatform.entity;

import com.baomidou.mybatisplus.annotation.*;
import com.fasterxml.jackson.annotation.JsonFormat;
import lombok.Data;

import java.time.OffsetDateTime;

@Data
@TableName("\"app_user\"")
public class User {
	@TableId(type = IdType.AUTO)
	private Long id;
	private String name;
	private Integer age;
	private String email;
	private String account;
	private String passwd;
	private String status;
	@TableField(value = "create_time", fill = FieldFill.INSERT)
	@JsonFormat(pattern = "yyyy-MM-dd HH:mm:ssXXX", timezone = "GMT+8")
	private OffsetDateTime createTime;
	@TableField(value = "update_time", fill = FieldFill.UPDATE)
	@JsonFormat(pattern = "yyyy-MM-dd HH:mm:ssXXX", timezone = "GMT+8")
	private OffsetDateTime updateTime;
}
