package auth

import "errors"

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrUserExists         = errors.New("user already exists")
	ErrGroupNotRegistered = errors.New("group not registered")
	ErrGroupExists        = errors.New("group already registered")
	ErrRankNotFound       = errors.New("rank not found")
	ErrInvalidInput       = errors.New("invalid input")
	ErrPermissionDenied   = errors.New("permission denied")
)
