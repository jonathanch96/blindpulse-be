package db

import (
	"context"

	"gorm.io/gorm"
)

type GormUnitOfWork struct{ db *gorm.DB }

func NewGormUnitOfWork(db *gorm.DB) *GormUnitOfWork { return &GormUnitOfWork{db: db} }

func (u *GormUnitOfWork) Do(ctx context.Context, fn func(context.Context) error) error {
	if FromContext(ctx, nil) != nil {
		return fn(ctx)
	}
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(WithContext(ctx, tx))
	})
}
