package model

import (
	"encoding/json"
	"sync"
	"util/dbutil/esutil"
)

type ESVideo struct {
	ID            string `json:"video_id,omitempty"`
	Title         string `json:"title,omitempty"`
	Description   string `json:"description,omitempty"`
	Duration      string `json:"duration,omitempty"`
	CoverPath     string `json:"cover_path,omitempty"`
	CntBarrages   int64  `json:"cnt_barrages,omitempty"`
	CntViewed     int64  `json:"cnt_viewed,omitempty"`
	PublisherName string `json:"publisher_name,omitempty"`
	VideoPath     string `json:"video_path,omitempty"`
	CreatedTime   string `json:"created_time,omitempty"`
}

var (
	mappingJson string
	once        sync.Once
)

func (e *ESVideo) GetMapping() string {
	once.Do(func() {
		var err error
		mapping := map[string]any{
			"mappings": esutil.EsEntry{
				"properties": esutil.EsEntry{
					"video_id": esutil.EsEntry{
						"type": "keyword",
					},
					"title": esutil.EsEntry{
						"type":     "text",
						"analyzer": "ik_max_word",
					},
					"description": esutil.EsEntry{
						"type":     "text",
						"analyzer": "ik_max_word",
					},
					"duration": esutil.EsEntry{
						"type":  "keyword",
						"index": false,
					},
					"cover_path": esutil.EsEntry{
						"type":  "keyword",
						"index": false,
					},
					"video_path": esutil.EsEntry{
						"type":  "keyword",
						"index": false,
					},
					"cnt_barrages": esutil.EsEntry{
						"type":  "long",
						"index": false,
					},
					"cnt_viewed": esutil.EsEntry{
						"type":  "long",
						"index": false,
					},
					"publisher_name": esutil.EsEntry{
						"type": "keyword",
					},
					"created_time": esutil.EsEntry{
						"type":   "date",
						"format": "yyyy-MM-dd HH:mm:ss",
					},
				},
			},
		}
		tmp, err := json.Marshal(mapping)
		if err != nil {
			panic(err)
		}
		mappingJson = string(tmp)
	})
	return mappingJson
}

func (e *ESVideo) GetIndexName() string {
	return "video"
}
