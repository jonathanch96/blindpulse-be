package equity_snapshots

import executiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/execution"

var _ executiondomain.SnapshotRepository = (*adapterGormPostgresql)(nil)
