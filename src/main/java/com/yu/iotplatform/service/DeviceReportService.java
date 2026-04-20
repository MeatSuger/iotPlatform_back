package com.yu.iotplatform.service;

import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.entity.DeviceStatusDTO;

public interface DeviceReportService {
    ApiResponse<String> reportStatus(String deviceId,
                                     String authorization,
                                     String deviceTokenHeader,
                                     DeviceStatusDTO statusDTO);

    ApiResponse<?> heartbeat(String deviceId,
                             String authorization,
                             String deviceTokenHeader);
}
