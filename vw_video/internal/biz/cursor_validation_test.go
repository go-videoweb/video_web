package biz

import (
	"encoding/json"
	"math"
	"testing"
	"time"
	comments "vw_video/api/v1/comment"
	interact "vw_video/api/v1/interact"
	"vw_video/internal/data/dal/model"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// cursorJSONForTest 使用独立 JSON 字段模拟接口输入，不再编码 Base64。
func cursorJSONForTest(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestCommentCursorTimestampValidation 验证缺少时间和非法协议时间不会被 AsTime 归一化后放行。
func TestCommentCursorTimestampValidation(t *testing.T) {
	for _, timestamp := range []*timestamppb.Timestamp{
		nil,
		{Seconds: 253402300800},
		{Seconds: 1700000000, Nanos: -1},
		{Seconds: 1700000000, Nanos: 1000000000},
	} {
		cursor := &comments.CommentCursor{
			VideoId: 9, SortBy: "created_at", Order: "desc", CommentId: 11, CreatedAt: timestamp,
		}
		if _, err := NewCommentQuery(9, 0, 7, "", "", cursor); err == nil {
			t.Fatalf("非法时间未被拒绝：%v", timestamp)
		}
	}
}

// assertCursorJSONKeys 验证服务端生成的游标使用完整语义字段，且不丢失大整数 ID。
func assertCursorJSONKeys(t *testing.T, cursor proto.Message, keys []string, idKey string) {
	t.Helper()
	raw, err := (protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}).Marshal(cursor)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != len(keys) {
		t.Fatalf("游标字段数量错误：%s", raw)
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			t.Fatalf("缺少语义字段 %q：%s", key, raw)
		}
	}
	if string(fields[idKey]) != `"9007199254740993"` {
		t.Fatalf("大 ID 编码错误：%s", fields[idKey])
	}
}

// TestCursorSemanticJSONKeys 验证评论和弹幕游标不再使用单字母标签。
func TestCursorSemanticJSONKeys(t *testing.T) {
	commentQuery, err := NewCommentQuery(9, 0, 7, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	commentCursor := commentQuery.NextCursor(&model.Comment{
		CommentID: 9007199254740993,
		CreatedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
		CntUpvote: 3,
	})
	assertCursorJSONKeys(t, commentCursor, []string{
		"video_id", "root_id", "sort_by", "order", "comment_id", "created_at", "upvote_count",
	}, "comment_id")

	barrageQuery, err := NewBarrageQuery(&interact.GetBarragesReq{
		VideoId: 9, UserId: 7, StartSecond: 0, EndSecond: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	barrageCursor := barrageQuery.NextCursor(&model.Barrage{
		BarrageID: 9007199254740993, PublishTime: 37,
	})
	assertCursorJSONKeys(t, barrageCursor, []string{
		"video_id", "start_second", "end_second", "position_second", "barrage_id",
	}, "barrage_id")
}

// TestCommentCursorValidationGroups 分别覆盖范围、排序位置、计数和 MySQL 时间边界。
func TestCommentCursorValidationGroups(t *testing.T) {
	tests := []struct {
		name   string // 被验证的边界场景。
		field  string // 本场景修改的游标字段。
		value  any    // 模拟客户端传入的字段值。
		sortBy string // 请求排序字段，空值表示按时间排序。
		valid  bool   // 是否应通过协议和业务校验。
	}{
		{name: "video mismatch", field: "video_id", value: int64(10)},
		{name: "root mismatch", field: "root_id", value: int64(1)},
		{name: "sort mismatch", field: "sort_by", value: "cnt_upvote"},
		{name: "order mismatch", field: "order", value: "asc"},
		{name: "zero comment ID", field: "comment_id", value: int64(0)},
		{name: "negative comment ID", field: "comment_id", value: int64(-1)},
		{name: "likes overflow", field: "upvote_count", value: uint64(math.MaxUint32) + 1},
		{name: "maximum likes", field: "upvote_count", value: uint64(math.MaxUint32), valid: true},
		{name: "zero time", field: "created_at", value: "0001-01-01T00:00:00Z"},
		{name: "year below MySQL range", field: "created_at", value: "0999-12-31T23:59:59Z"},
		{name: "minimum MySQL year", field: "created_at", value: "1000-01-01T00:00:00Z", valid: true},
		{name: "maximum MySQL year", field: "created_at", value: "9999-12-31T23:59:59Z", valid: true},
		{name: "malformed time", field: "created_at", value: "not a timestamp"},
		{name: "hot sort does not use time", field: "created_at", value: "0001-01-01T00:00:00Z", sortBy: "cnt_upvote", valid: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sortBy := tc.sortBy
			if sortBy == "" {
				sortBy = "created_at"
			}
			fields := map[string]any{
				"video_id":     int64(9),
				"root_id":      int64(0),
				"sort_by":      sortBy,
				"order":        "desc",
				"comment_id":   int64(11),
				"created_at":   "2026-11-01T12:00:00Z",
				"upvote_count": uint64(3),
			}
			fields[tc.field] = tc.value
			var cursor comments.CommentCursor
			err := protojson.Unmarshal(cursorJSONForTest(t, fields), &cursor)
			if err == nil {
				_, err = NewCommentQuery(9, 0, 7, sortBy, "desc", &cursor)
			}
			if tc.valid && err != nil {
				t.Fatalf("有效边界被拒绝：%v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("无效游标未拒绝")
			}
		})
	}
}

// TestBarrageCursorValidationGroups 验证视频、区间及左闭右开位置检查互不遗漏。
func TestBarrageCursorValidationGroups(t *testing.T) {
	tests := []struct {
		name  string // 被验证的播放位置或范围场景。
		field string // 本场景修改的游标字段。
		value any    // 模拟客户端传入的字段值。
		valid bool   // 是否应通过协议和业务校验。
	}{
		{name: "video mismatch", field: "video_id", value: int64(10)},
		{name: "start mismatch", field: "start_second", value: int32(11)},
		{name: "end mismatch", field: "end_second", value: int32(61)},
		{name: "zero barrage ID", field: "barrage_id", value: int64(0)},
		{name: "negative barrage ID", field: "barrage_id", value: int64(-1)},
		{name: "before interval", field: "position_second", value: int32(9)},
		{name: "at start", field: "position_second", value: int32(10), valid: true},
		{name: "before end", field: "position_second", value: int32(59), valid: true},
		{name: "at excluded end", field: "position_second", value: int32(60)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{
				"video_id":        int64(9),
				"start_second":    int32(10),
				"end_second":      int32(60),
				"position_second": int32(20),
				"barrage_id":      int64(11),
			}
			fields[tc.field] = tc.value
			var cursor interact.BarrageCursor
			err := protojson.Unmarshal(cursorJSONForTest(t, fields), &cursor)
			if err == nil {
				_, err = NewBarrageQuery(&interact.GetBarragesReq{
					VideoId: 9, UserId: 7, StartSecond: 10, EndSecond: 60, Cursor: &cursor,
				})
			}
			if tc.valid && err != nil {
				t.Fatalf("有效边界被拒绝：%v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("无效游标未拒绝")
			}
		})
	}
}
