package esutil

import (
	"fmt"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
)

const (
	Must    = "must"
	Should  = "should"
	Filter  = "filter"
	MustNot = "must_not"
)

// compositeHandlerMapping is a map.
// Its value is function that handles the bool Query's composite Query clauses to their respective handler functions, such as Must, Should, Filter, and MustNot.
var compositeHandlerMapping = map[string]func(es *EsSearchHelper, q *types.Query){
	Must: func(es *EsSearchHelper, q *types.Query) {
		es.Query.Bool.Must = append(es.Query.Bool.Must, *q)
	},
	Should: func(es *EsSearchHelper, q *types.Query) {
		es.Query.Bool.Should = append(es.Query.Bool.Should, *q)
	},
	Filter: func(es *EsSearchHelper, q *types.Query) {
		es.Query.Bool.Filter = append(es.Query.Bool.Filter, *q)
	},
	MustNot: func(es *EsSearchHelper, q *types.Query) {
		es.Query.Bool.MustNot = append(es.Query.Bool.MustNot, *q)
	},
}

func (es *EsSearchHelper) SetBoolTerm(field string, value any, compositeType string) *EsSearchHelper {
	if es.Query.Bool == nil {
		es.Query.Bool = types.NewBoolQuery()
	}
	q := types.NewQuery()
	q.Term[field] = types.TermQuery{Value: value}

	compositeHandler, ok := compositeHandlerMapping[compositeType]
	if !ok {
		panic(fmt.Sprintf("invalid compositeType: %s", compositeType))
	}
	compositeHandler(es, q)
	return es
}

// SetBoolMultiMatch sets the bool Query's multi match Query.
// NOTE: CompositeType MUST be one of Must, Should, Filter, or MustNot.
func (es *EsSearchHelper) SetBoolMultiMatch(query string, fields []string, compositeType string) *EsSearchHelper {
	if es.Query.Bool == nil {
		es.Query.Bool = types.NewBoolQuery()
	}
	q := types.NewQuery()
	q.MultiMatch = types.NewMultiMatchQuery()
	q.MultiMatch.Query = query
	q.MultiMatch.Fields = fields

	compositeHandler, ok := compositeHandlerMapping[compositeType]
	if !ok {
		panic(fmt.Sprintf("invalid compositeType: %s", compositeType))
	}
	compositeHandler(es, q)
	return es
}
