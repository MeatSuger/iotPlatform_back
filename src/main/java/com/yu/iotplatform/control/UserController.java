package com.yu.iotplatform.control;

import cn.dev33.satoken.annotation.SaCheckLogin;
import cn.dev33.satoken.annotation.SaCheckRole;
import cn.dev33.satoken.annotation.SaIgnore;
import cn.dev33.satoken.annotation.SaMode;
import cn.dev33.satoken.stp.SaTokenInfo;
import cn.dev33.satoken.stp.StpUtil;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.yu.iotplatform.entity.User;
import com.yu.iotplatform.service.UserService;
import jakarta.annotation.Resource;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.util.List;
import java.util.Objects;

/**
 * @brief 用户控制器，提供用户的注册、登录、查询、修改、删除等接口。
 * <p>
 * 权限规则：
 * - user → 只能操作自己；
 * - admin → 可操作他人但不能操作 super-admin；
 * - super-admin → 可操作所有用户。
 */
@RestController
@RequestMapping("/user")
public class UserController {

    @Resource
    private UserService userService;

    /**
     * @brief 用户注册接口（公开访问）。
     *
     * @param user 用户对象，包含账号和密码。
     * @return 注册结果信息。
     */
    @PostMapping("/register")
    @SaIgnore
    public ResponseEntity<String> register(@RequestBody User user) {
        if (user.getAccount() == null || user.getAccount().isBlank()
                || user.getPasswd() == null || user.getPasswd().isBlank()) {
            return ResponseEntity.badRequest().body("账号和密码不能为空");
        }

        User existUser = userService.getOne(
                new LambdaQueryWrapper<User>().eq(User::getAccount, user.getAccount())
        );
        if (existUser != null) {
            return ResponseEntity.badRequest().body("账号已存在");
        }

        return userService.save(user)
                ? ResponseEntity.ok("注册成功")
                : ResponseEntity.badRequest().body("注册失败");
    }

    /**
     * @brief 修改用户信息。
     * <p>
     * 权限：
     * - user → 只能修改自己；
     * - admin → 可修改他人但不能修改 super-admin；
     * - super-admin → 可修改任何人。
     *
     * @param user 待修改的用户对象。
     * @return 修改结果。
     */
    @PutMapping
    @SaCheckLogin
    public ResponseEntity<String> update(@RequestBody User user) {
        Long loginId = StpUtil.getLoginIdAsLong();
        String selfRole = getHighestRole(loginId);

        User target = userService.getById(user.getId());
        if (target == null) {
            return ResponseEntity.badRequest().body("目标用户不存在");
        }

        String targetRole = getHighestRole(target.getId());

        if ("user".equals(selfRole) && !loginId.equals(user.getId())) {
            return ResponseEntity.status(403).body("普通用户只能修改自己的信息");
        }

        if ("admin".equals(selfRole) && "super-admin".equals(targetRole)) {
            return ResponseEntity.status(403).body("管理员不能修改超级管理员信息");
        }

        userService.updateById(user);
        return ResponseEntity.ok("修改成功");
    }

    /**
     * @brief 查询单个用户信息。
     * <p>
     * 权限：
     * - user → 只能查询自己；
     * - admin → 可查询他人但不能查询 super-admin；
     * - super-admin → 可查询所有用户。
     *
     * @param id 用户ID。
     * @return 用户对象或错误状态。
     */
    @GetMapping("/profile")
    @SaCheckLogin
    public ResponseEntity<User> getProfile(@RequestParam Long id) {
        Long loginId = StpUtil.getLoginIdAsLong();
        String selfRole = getHighestRole(loginId);
        String targetRole = getHighestRole(id);

        if ("user".equals(selfRole) && !loginId.equals(id)) {
            return ResponseEntity.status(403).build();
        }

        if ("admin".equals(selfRole) && "super-admin".equals(targetRole)) {
            return ResponseEntity.status(403).build();
        }

        User user = userService.getById(id);
        if (user == null) {
            return ResponseEntity.notFound().build();
        }

        return ResponseEntity.ok(user);
    }

    /**
     * @brief 查询所有用户（仅 super-admin 可用）。
     *
     * @return 用户列表。
     */
    @GetMapping
    @SaCheckRole("super-admin")
    public ResponseEntity<List<User>> listAll() {
        return ResponseEntity.ok(userService.list());
    }

    /**
     * @brief 删除用户。
     * 权限：
     * - user → 无权限；
     * - admin → 可删他人但不能删 super-admin；
     * - super-admin → 可删任何人。
     *
     * @param id 待删除用户ID。
     * @return 删除结果。
     */
    @DeleteMapping("/{id}")
    @SaCheckLogin
    public ResponseEntity<String> delete(@PathVariable Long id) {
        Long loginId = StpUtil.getLoginIdAsLong();
        String selfRole = getHighestRole(loginId);
        String targetRole = getHighestRole(id);

        if ("user".equals(selfRole)) {
            return ResponseEntity.status(403).body("无权限删除用户");
        }

        if ("admin".equals(selfRole) && "super-admin".equals(targetRole)) {
            return ResponseEntity.status(403).body("管理员不能删除超级管理员");
        }

        boolean removed = userService.removeById(id);
        return removed ? ResponseEntity.ok("删除成功") : ResponseEntity.notFound().build();
    }

    /**
     * @brief 用户分页查询接口。
     * <p>
     * 权限：
     * - admin 和 super-admin 可使用。
     *
     * @param pageNum 页码。
     * @param pageSize 每页大小。
     * @param name 模糊查询用户名。
     * @return 分页结果。
     */
    @GetMapping("/page")
    @SaCheckRole(value = {"admin", "super-admin"}, mode = SaMode.OR)
    public ResponseEntity<Page<User>> findPage(
            @RequestParam(defaultValue = "0") Integer pageNum,
            @RequestParam(defaultValue = "10") Integer pageSize,
            @RequestParam(defaultValue = "") String name) {

        LambdaQueryWrapper<User> queryWrapper = new LambdaQueryWrapper<>();
        if (!name.isBlank()) {
            queryWrapper.like(User::getName, name);
        }

        Page<User> page = userService.page(new Page<>(pageNum, pageSize), queryWrapper);
        return ResponseEntity.ok(page);
    }

    /**
     * @brief 用户登录接口（公开访问）。
     *
     * @param account 用户账号。
     * @param passwd 用户密码。
     * @return 登录结果，包含 Token 信息。
     */
    @SaIgnore
    @PostMapping("/login")
    public ResponseEntity<Object> doLogin(@RequestParam String account,
                                          @RequestParam String passwd) {

        User user = userService.getByAccount(account);
        if (user == null || !Objects.equals(user.getPasswd(), passwd)) {
            return ResponseEntity.badRequest().body("用户账号或密码错误");
        }

        if (!"active".equals(user.getStatus())) {
            return ResponseEntity.badRequest().body("用户被封禁");
        }

        StpUtil.login(user.getId());
        SaTokenInfo tokenInfo = StpUtil.getTokenInfo();
        return ResponseEntity.ok(tokenInfo);
    }

    /**
     * @brief 查询当前登录状态（公开访问）。
     *
     * @return 当前会话登录状态。
     */
    @GetMapping("/isLogin")
    @SaIgnore
    public String isLogin() {
        return "当前会话是否登录：" + StpUtil.isLogin();
    }

    /**
     * @brief 获取指定用户的最高角色。
     *
     * @param userId 用户ID。
     * @return 用户最高角色（user、admin、super-admin）。
     */
    private String getHighestRole(Long userId) {
        List<String> roles = StpUtil.getRoleList(userId);
        if (roles.contains("super-admin")) return "super-admin";
        if (roles.contains("admin")) return "admin";
        return "user";
    }
}
