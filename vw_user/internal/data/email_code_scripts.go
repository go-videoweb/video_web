package data

import (
	"crypto/sha256"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// 邮箱相关键使用相同 hash tag，以保证 Redis Cluster 可以原子替换验证码和维护限额。
// 发送冷却及小时预算跨用途共享，重发或切换用途不能重置已有预算。
var storeEmailCode = redis.NewScript(`
-- KEYS[1]：邮箱发送冷却键；KEYS[2]：邮箱发送次数；KEYS[3]：当前用途的验证码哈希。
-- ARGV[1]：新验证码；ARGV[2]：有效期（秒）。返回 1 表示已签发，0 表示被限流。
if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
if tonumber(redis.call('GET', KEYS[2]) or '0') >= 5 then return 0 end

-- 从首次计数开始计算一小时，后续发送不延长这个窗口。
local n = redis.call('INCR', KEYS[2])
if n == 1 then redis.call('EXPIRE', KEYS[2], 3600) end
redis.call('SET', KEYS[1], '1', 'EX', 60)

-- 重发只替换当前用途的验证码并清零该码的错误次数，不影响邮箱的小时额度。
redis.call('HSET', KEYS[3], 'code', ARGV[1], 'attempts', 0)
redis.call('EXPIRE', KEYS[3], ARGV[2])
return 1
`)

// IP 预算使用独立单键脚本，先扣 IP 额度再尝试邮箱签发；拒绝时不退还额度，避免跨槽事务。
var reserveEmailCodeIP = redis.NewScript(`
-- KEYS[1]：来源 IP 的发送尝试计数，无 ARGV。返回 1 表示已占用额度，0 表示已达上限。
local n = tonumber(redis.call('GET', KEYS[1]) or '0')
if n >= 20 then return 0 end
n = redis.call('INCR', KEYS[1])
-- 只在首次占用额度时设置一小时有效期。
if n == 1 then redis.call('EXPIRE', KEYS[1], 3600) end
return 1
`)

// 同一验证码五次错误即失效，邮箱每小时最多二十次有效格式的验证尝试。
// 成功验证与删除在一个脚本中完成；新签发验证码不会重置小时验证预算。
var consumeEmailCode = redis.NewScript(`
-- KEYS[1]：当前用途的验证码哈希；KEYS[2]：邮箱跨用途的验证次数；ARGV[1]：用户输入。
-- 返回 1 表示校验通过且已消费，0 表示验证码无效，-1 表示邮箱验证次数已达上限。
if tonumber(redis.call('GET', KEYS[2]) or '0') >= 20 then return -1 end

-- 先计入邮箱额度，即使验证码不存在、已过期或已用过，本次尝试也占额度。
local n = redis.call('INCR', KEYS[2])
if n == 1 then redis.call('EXPIRE', KEYS[2], 3600) end
local code = redis.call('HGET', KEYS[1], 'code')
if not code then return 0 end

-- ~= 在 lua 脚本中表示 '不等于'
if code ~= ARGV[1] then
  -- 只有累计五次错误才删除验证码，单次输入错误不会让正确验证码失效。
  if redis.call('HINCRBY', KEYS[1], 'attempts', 1) >= 5 then redis.call('DEL', KEYS[1]) end
  return 0
end
-- 比对成功后在同一个脚本内删除，避免并发请求重复消费同一个验证码。
redis.call('DEL', KEYS[1])
return 1
`)

// emailCodeKey 隐藏键中的邮箱明文；同一邮箱的键共享 slot，suffix 区分用途和限额。
func emailCodeKey(email, suffix string) string {
	return fmt.Sprintf("email-code:{%x}:%s", sha256.Sum256([]byte(email)), suffix)
}
