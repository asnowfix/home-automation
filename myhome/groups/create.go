package groups

import (
	"context"
	"myhome"
)

func Create(ctx context.Context, group *myhome.GroupInfo) error {
	_, err := myhome.TheClient.CallE(ctx, myhome.GroupCreate, group)
	return err
}
