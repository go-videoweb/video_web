package biz

import (
	"context"
	"strings"
	"unicode/utf8"
	"util/dbutil/mysqlutil"
	"util/getid"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/domain"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)

// FollowPage 是关注内容分页，列表为零表示全部关注，而不是默认分组。
type FollowPage struct {
	Users             []*domain.UserSummary
	Total             int64
	PageNum, PageSize int32
}

// FollowRelation 表达当前用户对目标用户的关注状态，无关注时列表 ID 为零。
type FollowRelation struct {
	IsFollowing  bool
	FollowListID int64
}

// FollowRepo 定义关注及列表持久化边界，所有写入必须使用同一本地事务上下文。
type FollowRepo interface {
	LockFollowOwner(context.Context, int64) error
	CheckFollowTarget(context.Context, int64) error
	GetFollowLists(context.Context, int64) ([]*model.FollowList, error)
	FollowListNameExists(context.Context, int64, string, int64) (bool, error)
	CreateFollowList(context.Context, *model.FollowList) error
	RenameFollowList(context.Context, int64, int64, string) error
	SetDefaultFollowList(context.Context, int64, int64) error
	DeleteFollowLists(context.Context, int64, []int64) error
	GetFollowRelation(context.Context, int64, int64) (*model.UserFollow, error)
	CreateFollowPair(context.Context, *model.UserFollow) error
	DeleteFollowPairs(context.Context, int64, []int64) error
	GetFollowsInLists(context.Context, int64, []int64) ([]*model.UserFollow, error)
	MoveFollowee(context.Context, int64, int64, int64) error
	ChangeFollowCount(context.Context, int64, int64) error
	GetFolloweesPage(context.Context, int64, int64, int32, int32) (*FollowPage, error)
}

// FollowUsecase 在用户域内组合列表、双向关系和关注数量；粉丝数量由粉丝表统计。
type FollowUsecase struct {
	repo FollowRepo
	tx   mysqlutil.Transaction
}

// NewFollowUseCase 保留既有 Wire 构造参数，关注用例只依赖关注仓储和事务。
func NewFollowUseCase(repo FollowRepo, _ UserInfoRepo, tx mysqlutil.Transaction, _ log.Logger) *FollowUsecase {
	return &FollowUsecase{repo: repo, tx: tx}
}

// validFollowUser 拒绝无可信身份的请求，下游 RPC 仅允许由受信任网关调用。
func validFollowUser(id int64) error {
	if id <= 0 {
		return kerrors.BadRequest("INVALID_USER_ID", "用户 ID 必须为正数")
	}
	return nil
}

// followListName 统一名称边界，长度按字符计算，与当前 varchar(20) 对齐。
func followListName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || name == "" || utf8.RuneCountInString(name) > 20 {
		return "", kerrors.BadRequest("INVALID_FOLLOW_LIST_NAME", "关注列表名称需为 1 至 20 个字符")
	}
	return name, nil
}

// GetFollowLists 查询本人全部列表，并拒绝缺失或多个默认列表的异常数据。
func (u *FollowUsecase) GetFollowLists(ctx context.Context, userID int64) ([]*model.FollowList, error) {
	if err := validFollowUser(userID); err != nil {
		return nil, err
	}
	rows, err := u.repo.GetFollowLists(ctx, userID)
	if err != nil {
		return nil, err
	}
	defaults := 0
	for _, row := range rows {
		if row.UserID != userID {
			return nil, kerrors.InternalServer("INVALID_FOLLOW_LIST", "关注列表归属异常")
		}
		if row.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		return nil, kerrors.Conflict("DEFAULT_FOLLOW_LIST_CONFLICT", "默认关注列表必须恰好一个，请先核对数据")
	}
	return rows, nil
}

// resolveFollowList 读取本人列表；zeroDefault 为 true 时把零 ID 解析为当前默认列表。
func (u *FollowUsecase) resolveFollowList(ctx context.Context, userID, listID int64, zeroDefault bool) (*model.FollowList, error) {
	rows, err := u.GetFollowLists(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.ListID == listID || (zeroDefault && listID == 0 && row.IsDefault) {
			return row, nil
		}
	}
	return nil, kerrors.NotFound("FOLLOW_LIST_NOT_FOUND", "关注列表不存在")
}

// CreateFollowList 创建普通列表并返回已保存 ID，名称冲突由业务检查与数据库唯一键共同保护。
func (u *FollowUsecase) CreateFollowList(ctx context.Context, userID int64, name string) (int64, error) {
	if err := validFollowUser(userID); err != nil {
		return 0, err
	}
	name, err := followListName(name)
	if err != nil {
		return 0, err
	}
	id := getid.GetID()
	err = u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockFollowOwner(ctx, userID); err != nil {
			return err
		}
		if _, err := u.GetFollowLists(ctx, userID); err != nil {
			return err
		}
		exists, err := u.repo.FollowListNameExists(ctx, userID, name, 0)
		if err != nil {
			return err
		}
		if exists {
			return kerrors.Conflict("FOLLOW_LIST_NAME_EXISTS", "关注列表名称已存在")
		}
		return u.repo.CreateFollowList(ctx, &model.FollowList{ListID: id, UserID: userID, ListName: name, IsDefault: false})
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ModifyFollowList 仅修改名称，允许原名称，不通过此入口修改默认标记。
func (u *FollowUsecase) ModifyFollowList(ctx context.Context, userID, listID int64, name string) error {
	if err := validFollowUser(userID); err != nil {
		return err
	}
	if listID <= 0 {
		return kerrors.BadRequest("INVALID_FOLLOW_LIST", "关注列表 ID 必须为正数")
	}
	name, err := followListName(name)
	if err != nil {
		return err
	}
	return u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockFollowOwner(ctx, userID); err != nil {
			return err
		}
		row, err := u.resolveFollowList(ctx, userID, listID, false)
		if err != nil {
			return err
		}
		if row.ListName == name {
			return nil
		}
		exists, err := u.repo.FollowListNameExists(ctx, userID, name, listID)
		if err != nil {
			return err
		}
		if exists {
			return kerrors.Conflict("FOLLOW_LIST_NAME_EXISTS", "关注列表名称已存在")
		}
		return u.repo.RenameFollowList(ctx, userID, listID, name)
	})
}

// SetDefaultFollowList 原子切换唯一默认列表，不移动既有关注；重复设置当前默认列表成功。
func (u *FollowUsecase) SetDefaultFollowList(ctx context.Context, userID, listID int64) error {
	if err := validFollowUser(userID); err != nil {
		return err
	}
	if listID <= 0 {
		return kerrors.BadRequest("INVALID_FOLLOW_LIST", "关注列表 ID 必须为正数")
	}
	return u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockFollowOwner(ctx, userID); err != nil {
			return err
		}
		row, err := u.resolveFollowList(ctx, userID, listID, false)
		if err != nil {
			return err
		}
		if row.IsDefault {
			return nil
		}
		return u.repo.SetDefaultFollowList(ctx, userID, listID)
	})
}

// DeleteFollowLists 全有或全无地删除本人普通列表，同时解除对应关注和粉丝关系并扣减实际关注数。
// 输入 ID 去重且最多 100 个；任一缺失、他人列表或默认列表均拒绝整批请求。
func (u *FollowUsecase) DeleteFollowLists(ctx context.Context, userID int64, ids []int64) error {
	if err := validFollowUser(userID); err != nil {
		return err
	}
	if len(ids) == 0 || len(ids) > 100 {
		return kerrors.BadRequest("INVALID_FOLLOW_LIST_IDS", "一次需删除 1 至 100 个关注列表")
	}
	selected := make(map[int64]bool, len(ids))
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return kerrors.BadRequest("INVALID_FOLLOW_LIST_IDS", "关注列表 ID 必须为正数")
		}
		if !selected[id] {
			selected[id] = true
			unique = append(unique, id)
		}
	}
	return u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockFollowOwner(ctx, userID); err != nil {
			return err
		}
		lists, err := u.GetFollowLists(ctx, userID)
		if err != nil {
			return err
		}
		found := 0
		for _, list := range lists {
			if selected[list.ListID] {
				if list.IsDefault {
					return kerrors.Conflict("DEFAULT_FOLLOW_LIST_DELETE", "默认关注列表不可删除")
				}
				found++
			}
		}
		if found != len(unique) {
			return kerrors.NotFound("FOLLOW_LIST_NOT_FOUND", "至少一个关注列表不存在")
		}
		rows, err := u.repo.GetFollowsInLists(ctx, userID, unique)
		if err != nil {
			return err
		}
		targets := make([]int64, len(rows))
		for i, row := range rows {
			targets[i] = row.FollowUserID
		}
		if len(targets) > 0 {
			if err := u.repo.DeleteFollowPairs(ctx, userID, targets); err != nil {
				return err
			}
			if err := u.repo.ChangeFollowCount(ctx, userID, -int64(len(targets))); err != nil {
				return err
			}
		}
		return u.repo.DeleteFollowLists(ctx, userID, unique)
	})
}

// Follow 校验双方和分组后同步写入关注、粉丝记录；重复同组关注不重复增加关注数。
func (u *FollowUsecase) Follow(ctx context.Context, userID, targetID, listID int64) (*FollowRelation, error) {
	if err := validFollowUser(userID); err != nil {
		return nil, err
	}
	if targetID <= 0 || targetID == userID || listID < 0 {
		return nil, kerrors.BadRequest("INVALID_FOLLOW_TARGET", "不能关注自己，目标用户和分组 ID 必须有效")
	}
	var result *FollowRelation
	err := u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockFollowOwner(ctx, userID); err != nil {
			return err
		}
		if err := u.repo.CheckFollowTarget(ctx, targetID); err != nil {
			return err
		}
		list, err := u.resolveFollowList(ctx, userID, listID, true)
		if err != nil {
			return err
		}
		row, err := u.repo.GetFollowRelation(ctx, userID, targetID)
		if err != nil {
			return err
		}
		if row != nil {
			if row.FollowlistID != list.ListID {
				return kerrors.Conflict("FOLLOW_GROUP_CONFLICT", "已在其他分组关注，请使用分组移动接口")
			}
			result = &FollowRelation{IsFollowing: true, FollowListID: row.FollowlistID}
			return nil
		}
		if err := u.repo.CreateFollowPair(ctx, &model.UserFollow{UserID: userID, FollowUserID: targetID, FollowlistID: list.ListID}); err != nil {
			return err
		}
		if err := u.repo.ChangeFollowCount(ctx, userID, 1); err != nil {
			return err
		}
		result = &FollowRelation{IsFollowing: true, FollowListID: list.ListID}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Unfollow 解除当前关注和对应粉丝记录；无有效关注时成功且不扣减计数。
func (u *FollowUsecase) Unfollow(ctx context.Context, userID, targetID int64) error {
	if err := validFollowUser(userID); err != nil {
		return err
	}
	if targetID <= 0 || targetID == userID {
		return kerrors.BadRequest("INVALID_FOLLOW_TARGET", "目标用户 ID 无效")
	}
	return u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockFollowOwner(ctx, userID); err != nil {
			return err
		}
		row, err := u.repo.GetFollowRelation(ctx, userID, targetID)
		if err != nil {
			return err
		}
		if row == nil {
			return nil
		}
		if err := u.repo.DeleteFollowPairs(ctx, userID, []int64{targetID}); err != nil {
			return err
		}
		return u.repo.ChangeFollowCount(ctx, userID, -1)
	})
}

// GetFollowRelation 查询当前用户对目标的关系，明确返回是否关注及所属分组。
func (u *FollowUsecase) GetFollowRelation(ctx context.Context, userID, targetID int64) (*FollowRelation, error) {
	if err := validFollowUser(userID); err != nil {
		return nil, err
	}
	if targetID <= 0 {
		return nil, kerrors.BadRequest("INVALID_FOLLOW_TARGET", "目标用户 ID 无效")
	}
	if err := u.repo.CheckFollowTarget(ctx, targetID); err != nil {
		return nil, err
	}
	row, err := u.repo.GetFollowRelation(ctx, userID, targetID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return &FollowRelation{}, nil
	}
	return &FollowRelation{IsFollowing: true, FollowListID: row.FollowlistID}, nil
}

// MoveFollowee 只移动已存在的关注，不触及粉丝关系和双方计数。
func (u *FollowUsecase) MoveFollowee(ctx context.Context, userID, targetID, listID int64) error {
	if err := validFollowUser(userID); err != nil {
		return err
	}
	if targetID <= 0 || targetID == userID || listID <= 0 {
		return kerrors.BadRequest("INVALID_FOLLOW_TARGET", "目标用户和关注列表 ID 无效")
	}
	return u.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := u.repo.LockFollowOwner(ctx, userID); err != nil {
			return err
		}
		if err := u.repo.CheckFollowTarget(ctx, targetID); err != nil {
			return err
		}
		if _, err := u.resolveFollowList(ctx, userID, listID, false); err != nil {
			return err
		}
		row, err := u.repo.GetFollowRelation(ctx, userID, targetID)
		if err != nil {
			return err
		}
		if row == nil {
			return kerrors.NotFound("FOLLOW_NOT_FOUND", "尚未关注该用户")
		}
		if row.FollowlistID == listID {
			return nil
		}
		return u.repo.MoveFollowee(ctx, userID, targetID, listID)
	})
}

// GetFolloweesInfo 读取指定分组或全部关注，规范分页并返回总数，不以默认分组代替全部关注。
func (u *FollowUsecase) GetFolloweesInfo(ctx context.Context, userID, listID int64, page, size int32) (*FollowPage, error) {
	if err := validFollowUser(userID); err != nil {
		return nil, err
	}
	if listID < 0 || page < 0 || size < 0 {
		return nil, kerrors.BadRequest("INVALID_FOLLOW_PAGE", "分组和分页参数不可为负数")
	}
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if size > 100 || int64(page-1)*int64(size) > 2147483647 {
		return nil, kerrors.BadRequest("INVALID_FOLLOW_PAGE", "分页范围过大")
	}
	if listID > 0 {
		if _, err := u.resolveFollowList(ctx, userID, listID, false); err != nil {
			return nil, err
		}
	}
	return u.repo.GetFolloweesPage(ctx, userID, listID, page, size)
}
