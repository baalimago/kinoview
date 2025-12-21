package media

import "github.com/baalimago/kinoview/internal/agents"

func WithUserContextManager(m agents.UserContextManager) IndexerOption {
	return func(i *Indexer) {
		i.userContextMgr = m
	}
}
