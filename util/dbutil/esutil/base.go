package esutil

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
)

type EsSearchHelper struct {
	ctx       context.Context
	Query     *types.Query
	Sorters   []types.SortCombinations
	From      int
	Size      int
	Highlight *types.Highlight
	Source    *types.SourceConfig
	Aggs      map[string]types.Aggregations
	Index     Index
}

type EsEntry map[string]any

type Index interface {
	GetIndexName() string
	GetMapping() string
}

func NewEsSearchHelper(index Index) *EsSearchHelper {
	return &EsSearchHelper{
		Query: types.NewQuery(),
		Index: index,
	}
}

func (es *EsSearchHelper) WithContext(ctx context.Context) *EsSearchHelper {
	es.ctx = ctx
	return es
}

func (es *EsSearchHelper) SetSearchSize(size int) *EsSearchHelper {
	es.Size = size
	return es
}

func (es *EsSearchHelper) SetSearchFrom(from int) *EsSearchHelper {
	es.From = from
	return es
}

func (es *EsSearchHelper) Build() *search.Request {
	req := &search.Request{}

	if es.Query != nil {
		req.Query = es.Query
	}
	if len(es.Sorters) > 0 {
		req.Sort = es.Sorters
	}
	if es.Size > 0 {
		req.Size = &es.Size
	}
	if es.From > 0 {
		req.From = &es.From
	}
	if es.Highlight != nil {
		req.Highlight = es.Highlight
	}
	if es.Source != nil {
		req.Source_ = es.Source
	}
	if len(es.Aggs) > 0 {
		req.Aggregations = es.Aggs
	}

	return req
}

func (es *EsSearchHelper) PrintReq() *EsSearchHelper {
	var str bytes.Buffer
	req, err := json.Marshal(es.Build())
	if err != nil {
		panic(err)
	}
	json.Indent(&str, req, "", "  ")
	println(str.String())
	return es
}
