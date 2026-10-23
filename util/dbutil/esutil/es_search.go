package esutil

import (
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/operator"
)

func (es *EsSearchHelper) SetTerm(field string, value any) *EsSearchHelper {
	es.Query.Term[field] = types.TermQuery{Value: value}
	return es
}

// SetMultiMatch sets the multi match query.
// If fuzziness is empty, it will be set to "AUTO".
func (es *EsSearchHelper) SetMultiMatch(query string, op string, fuzziness string, fields ...string) *EsSearchHelper {
	q := es.Query
	q.MultiMatch = &types.MultiMatchQuery{
		Query:     query,
		Fields:    fields,
		Fuzziness: fuzziness,
		Operator: &operator.Operator{
			Name: op,
		},
	}
	if fuzziness == "" {
		q.MultiMatch.Fuzziness = "AUTO"
	}
	return es
}
