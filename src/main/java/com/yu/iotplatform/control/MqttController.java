package com.yu.iotplatform.control;

import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.entity.DeviceStatusDTO;
import com.yu.iotplatform.entity.mqtt.MqttClientStatus;
import com.yu.iotplatform.entity.mqtt.MqttMessageView;
import com.yu.iotplatform.entity.mqtt.MqttPublishRequest;
import com.yu.iotplatform.entity.mqtt.MqttSubscribeRequest;
import com.yu.iotplatform.entity.mqtt.MqttTopicRequest;
import com.yu.iotplatform.service.DeviceReportService;
import com.yu.iotplatform.service.MqttClientService;
import jakarta.annotation.Resource;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import java.util.List;

@RestController
@RequestMapping("/mqtt")
public class MqttController {
    @Resource
    private DeviceReportService deviceReportService;

    @Resource
    private MqttClientService mqttClientService;

    @PostMapping("/{deviceId}/Data")
    public ApiResponse<String> reportStatus(@PathVariable String deviceId,
                                            @RequestHeader(value = "Authorization", required = false) String authorization,
                                            @RequestHeader(value = "X-Device-Token", required = false) String deviceTokenHeader,
                                            @RequestBody DeviceStatusDTO statusDTO) {
        return deviceReportService.reportStatus(deviceId, authorization, deviceTokenHeader, statusDTO);
    }

    /**
     * MQTT 风格心跳上报
     */
    @PostMapping({"/{deviceId}/ping", "/{deviceId}/heartbeat"})
    public ApiResponse<?> heartbeat(@PathVariable String deviceId,
                                    @RequestHeader(value = "Authorization", required = false) String authorization,
                                    @RequestHeader(value = "X-Device-Token", required = false) String deviceTokenHeader) {
        return deviceReportService.heartbeat(deviceId, authorization, deviceTokenHeader);
    }

    /**
     * MQTT 客户端连接（连接参数由后端配置决定，支持遗言 LWT）
     */
    @PostMapping("/client/connect")
    public ApiResponse<MqttClientStatus> connect() {
        try {
            return ApiResponse.success("MQTT连接成功", mqttClientService.connect());
        } catch (IllegalArgumentException | IllegalStateException e) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), e.getMessage());
        }
    }

    @PostMapping("/client/disconnect")
    public ApiResponse<MqttClientStatus> disconnect() {
        return ApiResponse.success("MQTT已断开", mqttClientService.disconnect());
    }

    @PostMapping("/client/subscribe")
    public ApiResponse<MqttClientStatus> subscribe(@RequestBody MqttSubscribeRequest request) {
        try {
            return ApiResponse.success("订阅成功", mqttClientService.subscribe(request));
        } catch (IllegalArgumentException | IllegalStateException e) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), e.getMessage());
        }
    }

    @PostMapping("/client/unsubscribe")
    public ApiResponse<MqttClientStatus> unsubscribe(@RequestBody MqttTopicRequest request) {
        try {
            String topic = request == null ? null : request.getTopic();
            return ApiResponse.success("取消订阅成功", mqttClientService.unsubscribe(topic));
        } catch (IllegalArgumentException | IllegalStateException e) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), e.getMessage());
        }
    }

    @PostMapping("/client/publish")
    public ApiResponse<String> publish(@RequestBody MqttPublishRequest request) {
        try {
            mqttClientService.publish(request);
            return ApiResponse.success("发布成功", null);
        } catch (IllegalArgumentException | IllegalStateException e) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), e.getMessage());
        }
    }

    @GetMapping("/client/status")
    public ApiResponse<MqttClientStatus> status() {
        return ApiResponse.success(mqttClientService.status());
    }

    @GetMapping("/client/messages")
    public ApiResponse<List<MqttMessageView>> recentMessages(@RequestParam(defaultValue = "50") int limit) {
        return ApiResponse.success(mqttClientService.recentMessages(limit));
    }
}
