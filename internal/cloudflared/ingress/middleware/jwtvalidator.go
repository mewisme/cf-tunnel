package middleware

import (
	"context"
	"net/http"
)

type JWTValidator struct{}

func NewJWTValidator(teamName string, environment string, audTags []string) *JWTValidator {
	return &JWTValidator{}
}

func (v *JWTValidator) Name() string {
	return "AccessJWTValidator"
}

func (v *JWTValidator) Handle(_ context.Context, _ *http.Request) (*HandleResult, error) {
	return &HandleResult{
		ShouldFilterRequest: true,
		StatusCode:          http.StatusForbidden,
		Reason:              "access JWT not supported",
	}, nil
}
