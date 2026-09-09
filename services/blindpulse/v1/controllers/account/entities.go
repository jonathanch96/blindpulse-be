package account

import (
	accountdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/account"
)

type controller struct {
	accounts accountdomain.Service
}
