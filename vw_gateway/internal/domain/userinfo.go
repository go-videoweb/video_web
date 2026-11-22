package domain

import (
	"google.golang.org/protobuf/types/known/timestamppb"
)

type UserSummary struct {
	UserID     int64
	Username   string
	Signature  string
	Email      string
	Gender     int32
	AvatarPath string
	Birthday   *timestamppb.Timestamp
}
