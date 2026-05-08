package handlers

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

func TestRBACWriteErrorStatusMapsNotFoundAndConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "missing referenced role permission or group", err: pgx.ErrNoRows, want: http.StatusNotFound},
		{name: "foreign key violation", err: &pgconn.PgError{Code: "23503"}, want: http.StatusNotFound},
		{name: "unique violation", err: &pgconn.PgError{Code: "23505"}, want: http.StatusConflict},
		{name: "unexpected", err: errors.New("boom"), want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := rbacWriteErrorStatus(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}
