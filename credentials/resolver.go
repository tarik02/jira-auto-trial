package credentials

import (
	"context"
	"fmt"

	"github.com/tarik02/jira-auto-trial/config"
)

type Credentials struct {
	Username, Password string
}

func ResolveCredentials(ctx context.Context, account config.Account) (*Credentials, error) {
	switch true {
	case account.Plain != nil:
		return &Credentials{account.Plain.Username, account.Plain.Password}, nil

	default:
		return nil, fmt.Errorf("no credentials specified")
	}
}
