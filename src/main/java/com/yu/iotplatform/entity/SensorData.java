package com.yu.iotplatform.entity;

import com.baomidou.mybatisplus.annotation.FieldFill;
import com.baomidou.mybatisplus.annotation.TableField;
import com.fasterxml.jackson.annotation.JsonFormat;
import lombok.Data;

import java.time.LocalDateTime;

// ----------------- 传感器数据 -----------------
@Data
public class SensorData {
	private String name;        // 传感器名称
	private String type;        // 数据类型
	private Object value;       // 传感器数值
	@JsonFormat(pattern = "yyyy-MM-dd HH:mm:ss", timezone = "GMT+8")
	@TableField(fill = FieldFill.INSERT_UPDATE)
	private LocalDateTime timestamp; // 采集时间
}
