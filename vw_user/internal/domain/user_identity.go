package domain

import (
	"time"
	"vw_user/internal/data/dal/model"
)

type UserInfo struct {
	UserId     int64
	Username   string
	Email      string
	Password   string
	Signature  string
	Shells     int32
	CntLikes   int32
	CntFans    int32
	CntFollows int32
	CntVideos  int32
	AvatarPath string
	Gender     int32
	IsAdmin    bool
	Birthday   time.Time
}

func (u *UserInfo) padding(user *model.User) {
	u.UserId = user.UserID
	u.Username = user.Username
	u.Email = user.Email
	u.Password = user.Password
	u.Signature = user.Signature
	u.Shells = int32(user.Shells)
	u.CntLikes = int32(user.CntLikes)
	u.CntFollows = int32(user.CntFollows)
	u.Gender = int32(user.Gender)
	u.AvatarPath = user.AvatarPath
	u.IsAdmin = user.IsAdmin
	u.Birthday = user.Birthday
}

type UserSummary struct {
	FollowListID int64
	Id           int64
	Username     string
	Email        string
	Signature    string
	Gender       int32
	AvatarPath   string
	Birthday     time.Time
}

// padding pads the userbiz summary info with userbiz model
func (u *UserSummary) padding(user *model.User) {
	u.Id = user.UserID
	u.Username = user.Username
	u.Email = user.Email
	u.Signature = user.Signature
	u.Gender = int32(user.Gender)
	u.AvatarPath = user.AvatarPath
	u.Birthday = user.Birthday
}

func NewUserSummary(user *model.User) *UserSummary {
	userSummary := UserSummary{}
	userSummary.padding(user)
	return &userSummary
}

func NewUserSummaries(user ...*model.User) []*UserSummary {
	userSummaries := make([]*UserSummary, len(user))
	for i, u := range user {
		userSummaries[i] = new(UserSummary)
		userSummaries[i].padding(u)
	}
	return userSummaries
}

func NewUserInfo(user *model.User) *UserInfo {
	userInfo := &UserInfo{}
	userInfo.padding(user)
	return userInfo
}

func NewUserInfos(user ...*model.User) []*UserInfo {
	userInfos := make([]*UserInfo, len(user))
	for i, u := range user {
		userInfos[i] = new(UserInfo)
		userInfos[i].padding(u)
	}
	return userInfos
}
