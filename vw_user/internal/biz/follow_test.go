package biz

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"vw_user/internal/data/dal/model"
)

type followTestKey struct{}
type memoryFollowRepo struct {
	FollowRepo
	lists              map[int64]model.FollowList
	follows            map[int64]model.UserFollow
	fans               map[int64]bool
	count              int64
	fail               string
	locked             bool
	commits, rollbacks int
}

// newMemoryFollowRepo 创建隔离的默认、普通列表和空双向关系。
func newMemoryFollowRepo() *memoryFollowRepo {
	return &memoryFollowRepo{lists: map[int64]model.FollowList{13: {ListID: 13, UserID: 7, ListName: "默认", IsDefault: true}, 14: {ListID: 14, UserID: 7, ListName: "技术"}, 15: {ListID: 15, UserID: 7, ListName: "生活"}}, follows: map[int64]model.UserFollow{}, fans: map[int64]bool{}}
}

// WithTx 以独立副本模拟回滚，检查用例使用回调 ctx；不代表真实 MySQL 集成测试。
func (r *memoryFollowRepo) WithTx(ctx context.Context, fn func(context.Context) error) error {
	before := *r
	before.lists = make(map[int64]model.FollowList)
	for k, v := range r.lists {
		before.lists[k] = v
	}
	before.follows = make(map[int64]model.UserFollow)
	for k, v := range r.follows {
		before.follows[k] = v
	}
	before.fans = make(map[int64]bool)
	for k, v := range r.fans {
		before.fans[k] = v
	}
	r.locked = false
	err := fn(context.WithValue(ctx, followTestKey{}, true))
	if err == nil && r.fail == "commit" {
		err = errors.New("commit")
	}
	if err != nil {
		*r = before
		r.rollbacks++
		return err
	}
	r.commits++
	r.locked = false
	return nil
}

// WithSqlTx 不允许测试绕过本地事务抽象。
func (r *memoryFollowRepo) WithSqlTx(context.Context, func(context.Context) error) error {
	return errors.New("不支持 SQL 事务")
}

// checkWrite 检查回调身份和用户串行入口，允许按步骤注入失败。
func (r *memoryFollowRepo) checkWrite(ctx context.Context, stage string) error {
	if ctx.Value(followTestKey{}) != true || !r.locked {
		return errors.New("未使用事务 ctx 或未串行化")
	}
	if r.fail == stage {
		return errors.New(stage)
	}
	return nil
}

// LockFollowOwner 检查操作人和事务后模拟同用户写入口。
func (r *memoryFollowRepo) LockFollowOwner(ctx context.Context, id int64) error {
	if id != 7 || ctx.Value(followTestKey{}) != true {
		return errors.New("无可信事务身份")
	}
	r.locked = true
	return nil
}

// CheckFollowTarget 提供有效、缺失目标场景。
func (r *memoryFollowRepo) CheckFollowTarget(_ context.Context, id int64) error {
	if id == 99 {
		return errors.New("目标不存在")
	}
	return nil
}

// GetFollowLists 只返回属于操作人的列表。
func (r *memoryFollowRepo) GetFollowLists(_ context.Context, id int64) ([]*model.FollowList, error) {
	rows := make([]*model.FollowList, 0)
	for _, row := range r.lists {
		if row.UserID == id {
			copy := row
			rows = append(rows, &copy)
		}
	}
	return rows, nil
}

// FollowListNameExists 模拟同用户名称唯一检查。
func (r *memoryFollowRepo) FollowListNameExists(_ context.Context, id int64, name string, exclude int64) (bool, error) {
	for _, row := range r.lists {
		if row.UserID == id && row.ListID != exclude && strings.EqualFold(row.ListName, name) {
			return true, nil
		}
	}
	return false, nil
}

// CreateFollowList 保存测试列表，验证创建使用回调事务。
func (r *memoryFollowRepo) CreateFollowList(ctx context.Context, row *model.FollowList) error {
	if err := r.checkWrite(ctx, "create"); err != nil {
		return err
	}
	r.lists[row.ListID] = *row
	return nil
}

// RenameFollowList 模拟名称修改。
func (r *memoryFollowRepo) RenameFollowList(ctx context.Context, _ int64, id int64, name string) error {
	if err := r.checkWrite(ctx, "rename"); err != nil {
		return err
	}
	row := r.lists[id]
	row.ListName = name
	r.lists[id] = row
	return nil
}

// SetDefaultFollowList 模拟同事务内默认切换。
func (r *memoryFollowRepo) SetDefaultFollowList(ctx context.Context, _ int64, id int64) error {
	if err := r.checkWrite(ctx, "default"); err != nil {
		return err
	}
	for key, row := range r.lists {
		row.IsDefault = key == id
		r.lists[key] = row
	}
	return nil
}

// DeleteFollowLists 模拟列表删除，可在关系和计数更新后注入失败。
func (r *memoryFollowRepo) DeleteFollowLists(ctx context.Context, _ int64, ids []int64) error {
	if err := r.checkWrite(ctx, "delete-lists"); err != nil {
		return err
	}
	for _, id := range ids {
		delete(r.lists, id)
	}
	return nil
}

// GetFollowRelation 返回测试有效关系。
func (r *memoryFollowRepo) GetFollowRelation(_ context.Context, _, target int64) (*model.UserFollow, error) {
	row, ok := r.follows[target]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

// CreateFollowPair 模拟双向关系写入，可在关注已写后制造粉丝写入失败。
func (r *memoryFollowRepo) CreateFollowPair(ctx context.Context, row *model.UserFollow) error {
	if err := r.checkWrite(ctx, "follow"); err != nil {
		return err
	}
	r.follows[row.FollowUserID] = *row
	if r.fail == "fan" {
		return errors.New("fan")
	}
	r.fans[row.FollowUserID] = true
	return nil
}

// DeleteFollowPairs 同时移除对应的双向关系。
func (r *memoryFollowRepo) DeleteFollowPairs(ctx context.Context, _ int64, ids []int64) error {
	if err := r.checkWrite(ctx, "delete-pairs"); err != nil {
		return err
	}
	for _, id := range ids {
		delete(r.follows, id)
		delete(r.fans, id)
	}
	return nil
}

// GetFollowsInLists 只收集待删除列表下的有效目标。
func (r *memoryFollowRepo) GetFollowsInLists(_ context.Context, _ int64, ids []int64) ([]*model.UserFollow, error) {
	rows := make([]*model.UserFollow, 0)
	for _, row := range r.follows {
		for _, id := range ids {
			if row.FollowlistID == id {
				copy := row
				rows = append(rows, &copy)
			}
		}
	}
	return rows, nil
}

// MoveFollowee 模拟只修改分组。
func (r *memoryFollowRepo) MoveFollowee(ctx context.Context, _, target, list int64) error {
	if err := r.checkWrite(ctx, "move"); err != nil {
		return err
	}
	row := r.follows[target]
	row.FollowlistID = list
	r.follows[target] = row
	return nil
}

// ChangeFollowCount 模拟精确计数和下溢保护。
func (r *memoryFollowRepo) ChangeFollowCount(ctx context.Context, _, delta int64) error {
	if err := r.checkWrite(ctx, "count"); err != nil {
		return err
	}
	if r.count+delta < 0 {
		return errors.New("计数下溢")
	}
	r.count += delta
	return nil
}

// TestFollowLifecycleOffline 验证默认归属、双向写入、幂等、分组移动、状态回读和取消。
func TestFollowLifecycleOffline(t *testing.T) {
	r := newMemoryFollowRepo()
	u := NewFollowUseCase(r, nil, r, nil)
	ctx := context.Background()
	state, err := u.Follow(ctx, 7, 9, 0)
	if err != nil || !state.IsFollowing || state.FollowListID != 13 || r.count != 1 || !r.fans[9] {
		t.Fatalf("默认关注错误：%v %v", state, err)
	}
	if _, err := u.Follow(ctx, 7, 9, 0); err != nil || r.count != 1 {
		t.Fatal("重复关注改变计数", err)
	}
	if _, err := u.Follow(ctx, 7, 9, 14); err == nil {
		t.Fatal("跨分组重复关注未拒绝")
	}
	if err := u.MoveFollowee(ctx, 7, 9, 14); err != nil || r.count != 1 || !r.fans[9] {
		t.Fatal("移动改变关系计数", err)
	}
	state, err = u.GetFollowRelation(ctx, 7, 9)
	if err != nil || state.FollowListID != 14 {
		t.Fatal("移动状态回读失败", err)
	}
	if err := u.SetDefaultFollowList(ctx, 7, 15); err != nil {
		t.Fatal(err)
	}
	if r.follows[9].FollowlistID != 14 {
		t.Fatal("默认切换迁移了已有关注")
	}
	state, err = u.Follow(ctx, 7, 10, 0)
	if err != nil || state.FollowListID != 15 {
		t.Fatal("新默认分组未生效", err)
	}
	if err := u.Unfollow(ctx, 7, 9); err != nil {
		t.Fatal(err)
	}
	if err := u.Unfollow(ctx, 7, 9); err != nil || r.count != 1 || r.fans[9] {
		t.Fatal("重复取消扣减计数", err)
	}
	state, err = u.GetFollowRelation(ctx, 7, 9)
	if err != nil || state.IsFollowing || state.FollowListID != 0 {
		t.Fatal("取消状态不正确")
	}
}

// TestFollowAtomicFailuresOffline 验证粉丝写入、计数、最终列表删除及提交失败均整体回滚。
func TestFollowAtomicFailuresOffline(t *testing.T) {
	for _, stage := range []string{"fan", "count", "commit"} {
		t.Run(stage, func(t *testing.T) {
			r := newMemoryFollowRepo()
			r.fail = stage
			u := NewFollowUseCase(r, nil, r, nil)
			if state, err := u.Follow(context.Background(), 7, 9, 14); err == nil || state != nil || len(r.follows) != 0 || len(r.fans) != 0 || r.count != 0 {
				t.Fatal("关注未整体回滚", err)
			}
		})
	}
	for _, stage := range []string{"", "delete-lists", "count", "commit"} {
		t.Run("批量"+stage, func(t *testing.T) {
			r := newMemoryFollowRepo()
			u := NewFollowUseCase(r, nil, r, nil)
			ctx := context.Background()
			for _, target := range []int64{9, 10} {
				if _, err := u.Follow(ctx, 7, target, 14); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := u.Follow(ctx, 7, 11, 15); err != nil {
				t.Fatal(err)
			}
			r.fail = stage
			err := u.DeleteFollowLists(ctx, 7, []int64{14, 14, 15})
			if stage == "" {
				if err != nil || r.count != 0 || len(r.fans) != 0 || len(r.follows) != 0 || len(r.lists) != 1 {
					t.Fatal("批量删除未按实际关系计数", err)
				}
			} else if err == nil || r.count != 3 || len(r.follows) != 3 || len(r.fans) != 3 || len(r.lists) != 3 {
				t.Fatal("批量删除没有整体回滚", err)
			}
		})
	}
}

// TestFollowListValidationOffline 验证名称、归属、默认列表保护及无效目标均不会部分改写。
func TestFollowListValidationOffline(t *testing.T) {
	ctx := context.Background()
	r := newMemoryFollowRepo()
	u := NewFollowUseCase(r, nil, r, nil)
	id, err := u.CreateFollowList(ctx, 7, "  摄影  ")
	if err != nil || id <= 0 || r.lists[id].ListName != "摄影" || r.lists[id].IsDefault {
		t.Fatal("创建普通列表错误", err)
	}
	if _, err := u.CreateFollowList(ctx, 7, "摄影"); err == nil {
		t.Fatal("同名创建未拒绝")
	}
	if _, err := u.CreateFollowList(ctx, 7, " "); err == nil {
		t.Fatal("空名称未拒绝")
	}
	if err := u.ModifyFollowList(ctx, 7, id, "摄影"); err != nil {
		t.Fatal("原名称不幂等", err)
	}
	if err := u.ModifyFollowList(ctx, 7, id, "技术"); err == nil {
		t.Fatal("修改重名未拒绝")
	}
	if err := u.ModifyFollowList(ctx, 7, id, strings.Repeat("字", 21)); err == nil {
		t.Fatal("超长名称未拒绝")
	}
	for _, ids := range [][]int64{{13}, {14, 99}, {14, 13}, {0}, {}} {
		if err := u.DeleteFollowLists(ctx, 7, ids); err == nil {
			t.Fatalf("无效删除成功：%v", ids)
		}
	}
	for _, target := range []int64{0, 7, 99} {
		if _, err := u.Follow(ctx, 7, target, 0); err == nil {
			t.Fatal("无效目标未拒绝")
		}
	}
	if _, err := u.Follow(ctx, 7, 9, 99); err == nil {
		t.Fatal("他人或不存在列表被使用")
	}
	if _, err := u.GetFolloweesInfo(ctx, 7, 0, -1, 20); err == nil {
		t.Fatal("负分页未拒绝")
	}
	if _, err := u.GetFolloweesInfo(ctx, 7, 0, 1, 101); err == nil {
		t.Fatal("超大分页未拒绝")
	}
	defaults := []int64{}
	for id, row := range r.lists {
		if row.IsDefault {
			defaults = append(defaults, id)
		}
	}
	sort.Slice(defaults, func(i, j int) bool { return defaults[i] < defaults[j] })
	if !reflect.DeepEqual(defaults, []int64{13}) {
		t.Fatal("默认标记被意外修改")
	}
	row := r.lists[14]
	row.IsDefault = true
	r.lists[14] = row
	if _, err := u.GetFollowLists(ctx, 7); err == nil {
		t.Fatal("多个默认列表未拒绝")
	}
}
