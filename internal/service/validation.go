package service

import (
	"encoding/hex"
	"math"
	"strings"

	"community-waste/internal/domain"
)

func validUUID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil && len(decoded) == 16
}

func validPagination(pagination domain.Pagination) bool {
	return pagination.Page >= 1 && pagination.Limit >= 1 && pagination.Limit <= 100 &&
		pagination.Page-1 <= math.MaxInt64/pagination.Limit
}
