package cli

import "context"

func Execute(ctx context.Context, cmd Command, args []string) int { return execute(ctx, cmd, args) }
