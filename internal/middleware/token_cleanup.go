package middleware

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/sa-tokens/sa-token-go/core/listener"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"
)

// RegisterKickedTokenCleanup 注册 token 物理清理监听器：
// sa-token 的 Kickout（被踢下线）/ Replaced（被顶号）默认只把 token key 标记为
// KICK_OUT / BE_REPLACED 并保留原 TTL（设备 token NeverExpire 时会永久残留），
// 此处监听事件并直接从 Redis 删除 token key，做到"踢下线即删除"。
func RegisterKickedTokenCleanup(mgr *sagin.Manager, rdb *redis.Client) {
	prefix := mgr.GetConfig().KeyPrefix
	del := func(d *listener.EventData) {
		if d.Token == "" {
			return
		}
		_ = rdb.Del(context.Background(), prefix+"token:"+d.Token)
	}
	mgr.RegisterFunc(listener.EventKickout, del)
	mgr.RegisterFunc(listener.EventReplaced, del)
}
