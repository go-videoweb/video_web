package data

import (
	"crypto/sha256"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// 邮箱相关键使用相同 hash tag，以保证 Redis Cluster 可以原子替换验证码和维护限额。
// 发送冷却及小时预算跨用途共享，重发或切换用途不能重置已有预算。
var storeEmailCode = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
if tonumber(redis.call('GET', KEYS[2]) or '0') >= 5 then return 0 end
local n = redis.call('INCR', KEYS[2])
if n == 1 then redis.call('EXPIRE', KEYS[2], 3600) end
redis.call('SET', KEYS[1], '1', 'EX', 60)
redis.call('HSET', KEYS[3], 'code', ARGV[1], 'attempts', 0)
redis.call('EXPIRE', KEYS[3], ARGV[2])
return 1
`)

// IP 预算使用独立单键脚本，先扣 IP 额度再尝试邮箱签发；拒绝时不退还额度，避免跨槽事务。
var reserveEmailCodeIP = redis.NewScript(`
local n = tonumber(redis.call('GET', KEYS[1]) or '0')
if n >= 20 then return 0 end
n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('EXPIRE', KEYS[1], 3600) end
return 1
`)

// 同一验证码五次错误即失效，邮箱每小时最多二十次有效格式的验证尝试。
// 成功验证与删除在一个脚本中完成；新签发验证码不会重置小时验证预算。
var consumeEmailCode = redis.NewScript(`
if tonumber(redis.call('GET', KEYS[2]) or '0') >= 20 then return -1 end
local n = redis.call('INCR', KEYS[2])
if n == 1 then redis.call('EXPIRE', KEYS[2], 3600) end
local code = redis.call('HGET', KEYS[1], 'code')
if not code then return 0 end
if code ~= ARGV[1] then
  if redis.call('HINCRBY', KEYS[1], 'attempts', 1) >= 5 then redis.call('DEL', KEYS[1]) end
  return 0
end
redis.call('DEL', KEYS[1])
return 1
`)

// emailCodeKey 隐藏键中的邮箱明文；同一邮箱的键共享 slot，suffix 区分用途和限额。
func emailCodeKey(email, suffix string) string {
	return fmt.Sprintf("email-code:{%x}:%s", sha256.Sum256([]byte(email)), suffix)
}
