package internal

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const (
	// The server clamps size to 20 although the spec declares a maximum of 100.
	pageSize     = 20
	maxPageCount = 1000
)

// The walk returns this whole, so it is the diagnostic a program reads.
const pageCapMessage = "The MailSlurp `%s` walk read %d pages and reached no empty page."

// pageEnvelope is the page the API returns. It also carries totalPages and totalElements, and
// both are permanently -1, so nothing here reads them.
type pageEnvelope[T any] struct {
	Content []T  `json:"content"`
	Empty   bool `json:"empty"`
}

// walkPages reads every page of a paginated endpoint. It stops on the first empty page, because
// the `last` flag stays false until the walk passes the end.
func walkPages[T any](
	ctx context.Context, c *client, operation, path string, query url.Values,
) ([]T, error) {
	collected := []T{}

	for number := 0; number < maxPageCount; number++ {
		pageQuery := url.Values{}
		for key, values := range query {
			pageQuery[key] = values
		}
		pageQuery.Set("page", strconv.Itoa(number))
		pageQuery.Set("size", strconv.Itoa(pageSize))

		page, err := call[pageEnvelope[T]](ctx, c, apiRequest{
			method:    http.MethodGet,
			path:      path,
			operation: operation,
			query:     pageQuery,
		})
		if err != nil {
			return nil, err
		}
		if page.Empty || len(page.Content) == 0 {
			return collected, nil
		}
		collected = append(collected, page.Content...)
	}

	//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
	return nil, fmt.Errorf(pageCapMessage, operation, maxPageCount)
}
