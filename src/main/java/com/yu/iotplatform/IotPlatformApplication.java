package com.yu.iotplatform;


import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;


@SpringBootApplication
@RestController
@RequestMapping("/index")
public class IotPlatformApplication {

	public static void main(String[] args) {
		SpringApplication.run(IotPlatformApplication.class, args);
	}

//
//    /***
//     * hello
//     * @return
//     */
//    @RequestMapping("/hello")
//    public String sayHello() {
//        return "Hello World";
//    }
//
////    @GetMapping
////    public String index() {
////        return "GET 无参请求api方法已实现";
////    }
//
//    @GetMapping("/{id}")
//    public String index(@PathVariable String id) {
//        System.out.printf("id: %s\n", id);
//        return "GET Restful请求传值的方法实现成功\n";
//    }
//
//    @GetMapping
//    public String index2(@RequestParam String id, @RequestParam String name) {
//        System.out.printf("id: %s\n", id);
//        System.out.printf("name: %s\n", name);
//        return "GET 普通请求传值方法已经实现了";
//    }
//
//    @PostMapping
//    public String save(@RequestBody Map<String, String> map) {
//        System.out.printf(map.toString());
//        return "POST请求接收成功";
//    }
//
//    @PutMapping("/{id}")
//    public String update(@PathVariable String id, @RequestBody Map<String, String> map) {
//        System.out.printf("id: %s, name=%s\n", id, map);
//        return "PUT请求接收成功";
//    }
//
//    @DeleteMapping("/{id}")
//    public String delete(@PathVariable String id) {
//        System.out.printf("id: %s\n", id);
//        return "DELET请求接收成功";
//    }
}
