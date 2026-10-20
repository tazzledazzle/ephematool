//go:build integration

package store_test

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func pgInt8(v int64) pgtype.Int8 { return pgtype.Int8{Int64: v, Valid: true} }

func pgTime(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
