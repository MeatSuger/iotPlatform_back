package com.yu.iotplatform.control;

import cn.dev33.satoken.annotation.SaCheckLogin;
import cn.dev33.satoken.annotation.SaCheckRole;
import cn.dev33.satoken.annotation.SaIgnore;
import cn.dev33.satoken.annotation.SaMode;
import cn.dev33.satoken.stp.SaTokenInfo;
import cn.dev33.satoken.stp.StpUtil;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.extension.plugins.pagination.Page;
import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.entity.User;
import com.yu.iotplatform.service.UserService;
import jakarta.annotation.Resource;
import org.springframework.http.HttpStatus;
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
    public static final String USER_STATUS_ACTIVE = "ACTIVE";
    public static final String USER_STATUS_BANDER = "BANDED";
    @Resource
    private UserService userService;

    /**
     * @param user 用户对象，包含账号和密码。
     * @return 注册结果信息。
     * @brief 用户注册接口（公开访问）。
     */
    @PostMapping("/register")
    @SaIgnore
    public ApiResponse<String> register(@RequestBody User user) {
        if (user.getAccount() == null || user.getAccount().isBlank()
                || user.getPasswd() == null || user.getPasswd().isBlank()) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "账号和密码不能为空");
        }

        User existUser = userService.getOne(
                new LambdaQueryWrapper<User>().eq(User::getAccount, user.getAccount())
        );
        if (existUser != null) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "账号已存在");
        }
        user.setStatus(USER_STATUS_ACTIVE);
        return userService.save(user)
                ? ApiResponse.success("注册成功", null)
                : ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "注册失败");
    }

    /**
     * @param user 待修改的用户对象。
     * @return 修改结果。
     * @brief 修改用户信息。
     * <p>
     * 权限：
     * - user → 只能修改自己；
     * - admin → 可修改他人但不能修改 super-admin；
     * - super-admin → 可修改任何人。
     */
    @PutMapping
    @SaCheckLogin
    public ApiResponse<String> update(@RequestBody User user) {
        Long loginId = StpUtil.getLoginIdAsLong();
        String selfRole = getHighestRole(loginId);

        User target = userService.getById(user.getId());
        if (target == null) {
            return ApiResponse.fail(400, "目标用户不存在");
        }

        String targetRole = getHighestRole(target.getId());

        if ("user".equals(selfRole) && !loginId.equals(user.getId())) {
            return ApiResponse.fail(403, "普通用户只能修改自己的信息");
        }

        if ("admin".equals(selfRole) && "super-admin".equals(targetRole)) {
            return ApiResponse.fail(403, "管理员不能修改超级管理员信息");
        }

        userService.updateById(user);
        return ApiResponse.success("修改成功", null);
    }

    /**
     * @param id 用户ID。
     * @return 用户对象或错误状态。
     * @brief 查询单个用户信息。
     * <p>
     * 权限：
     * - user → 只能查询自己；
     * - admin → 可查询他人但不能查询 super-admin；
     * - super-admin → 可查询所有用户。
     * <p/>
     */
    @GetMapping("/profile")
    @SaCheckLogin
    public ApiResponse<User> getProfile(@RequestParam Long id) {
        Long loginId = StpUtil.getLoginIdAsLong();
        String selfRole = getHighestRole(loginId);
        String targetRole = getHighestRole(id);

        if ("user".equals(selfRole) && !loginId.equals(id)) {
            return ApiResponse.fail(403, "无权限查看他人信息");
        }

        if ("admin".equals(selfRole) && "super-admin".equals(targetRole)) {
            return ApiResponse.fail(403, "管理员不能查看超级管理员信息");
        }

        User user = userService.getById(id);
        if (user == null) {
            return ApiResponse.fail(404, "用户不存在");
        }

        return ApiResponse.success(user);
    }

    /**
     * @return 用户列表。
     * @brief 查询所有用户（仅 super-admin 可用）。
     */
    @GetMapping("/list")
    @SaCheckRole("super-admin")
    public ApiResponse<List<User>> listAll() {
        return ApiResponse.success(userService.list());
    }

    /**
     * @param id 待删除用户ID。
     * @return 删除结果。
     * @brief 删除用户。
     * 权限：
     * - user → 无权限；
     * - admin → 可删他人但不能删 super-admin；
     * - super-admin → 可删任何人。
     */
    @PostMapping("/delete")
    @SaCheckLogin
    public ApiResponse<String> delete(@RequestParam Long id) {
        Long loginId = StpUtil.getLoginIdAsLong();
        String selfRole = getHighestRole(loginId);
        String targetRole = getHighestRole(id);

        if ("user".equals(selfRole)) {
            return ApiResponse.fail(403, "无权限删除用户");
        }

        if ("admin".equals(selfRole) && "super-admin".equals(targetRole)) {
            return ApiResponse.fail(403, "管理员不能删除超级管理员");
        }

        boolean removed = userService.removeById(id);
        if (!removed) {
            return ApiResponse.fail(404, "用户不存在或删除失败");
        }

        return ApiResponse.success("删除成功", null);
    }


    /**
     * @param pageNum  页码。
     * @param pageSize 每页大小。
     * @param name     模糊查询用户名。
     * @return 分页结果。
     * @brief 用户分页查询接口。
     * <p>
     * 权限：
     * - admin 和 super-admin 可使用。
     */
    @GetMapping("/page")
    @SaCheckRole(value = {"admin", "super-admin"}, mode = SaMode.OR)
    public ApiResponse<Page<User>> findPage(
            @RequestParam(defaultValue = "0") Integer pageNum,
            @RequestParam(defaultValue = "10") Integer pageSize,
            @RequestParam(defaultValue = "") String name) {

        LambdaQueryWrapper<User> queryWrapper = new LambdaQueryWrapper<>();
        if (!name.isBlank()) {
            queryWrapper.like(User::getName, name);
        }

        Page<User> page = userService.page(new Page<>(pageNum, pageSize), queryWrapper);
        return ApiResponse.success(page);
    }

    /**
     * @param account 用户账号。
     * @param passwd  用户密码。
     * @return 登录结果，包含 Token 信息。
     * @brief 用户登录接口（公开访问）。
     */
    @SaIgnore
    @PostMapping("/login")
    public ApiResponse<Object> doLogin(@RequestParam String account,
                                       @RequestParam String passwd) {

        User user = userService.getByAccount(account);
        if (user == null) {
            return ApiResponse.fail(400, "用户未注册");
        }
        if (!Objects.equals(user.getPasswd(), passwd)) {
            return ApiResponse.fail(400, "密码错误");
        }

        if (!USER_STATUS_ACTIVE.equals(user.getStatus())) {
            return ApiResponse.fail(403, "用户被封禁");
        }

        // 如果当前请求已携带同账号有效 token，则直接复用，避免重复登录触发旧 token 注销
        if (StpUtil.isLogin()) {
            Object currentLoginId = StpUtil.getLoginIdDefaultNull();
            if (currentLoginId != null && Objects.equals(String.valueOf(currentLoginId), String.valueOf(user.getId()))) {
                SaTokenInfo tokenInfo = StpUtil.getTokenInfo();
                return ApiResponse.success("登录成功", tokenInfo);
            }
            // 当前已登录但不是同一账号，先清理再登录目标账号
            StpUtil.logout();
        }

        StpUtil.login(user.getId());
        getHighestRole(user.getId());
        SaTokenInfo tokenInfo = StpUtil.getTokenInfo();
        return ApiResponse.success("登录成功", tokenInfo);
    }

    /**
     * @return 当前会话登录状态。
     * @brief 查询当前登录状态（公开访问）。
     */
    @GetMapping("/isLogin")
    @SaIgnore
    public ApiResponse<String> isLogin() {
        return ApiResponse.success(StpUtil.isLogin() ? "已登录" : "未登录", null);
    }

    /**
     * @param userId 用户ID。
     * @return 用户最高角色（user、admin、super-admin）。
     * @brief 获取指定用户的最高角色。
     */
    private String getHighestRole(Long userId) {
        List<String> roles = StpUtil.getRoleList(userId);
        if (roles.contains("super-admin")) return "super-admin";
        if (roles.contains("admin")) return "admin";
        return "user";
    }
}
