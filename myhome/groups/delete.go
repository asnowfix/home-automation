package groups

import (
	"context"
	"myhome"
)

func Delete(ctx context.Context, group string) error {
	_, err := myhome.TheClient.CallE(ctx, myhome.GroupDelete, group)
	return err
}
