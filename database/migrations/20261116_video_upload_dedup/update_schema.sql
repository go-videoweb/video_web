-- 同一发布者的标题唯一，标题按 video.title 当前排序规则比较。
-- 已软删除的视频仍占用标题。已有同名行时 ALTER 会失败，需人工确认保留哪条。
ALTER TABLE video
    ADD CONSTRAINT uq_video_publisher_title UNIQUE (publisher_id, title);
