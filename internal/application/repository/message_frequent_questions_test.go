package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGetFrequentlyAskedQuestionsOnlyIncludesTenantWidgetMessages(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, content TEXT, role TEXT, created_at DATETIME, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE integration_chat_bindings (session_id TEXT PRIMARY KEY, client_id TEXT NOT NULL, user_id TEXT NOT NULL, source TEXT NOT NULL)`).Error)

	now := time.Now()
	require.NoError(t, db.Exec(`INSERT INTO sessions (id, tenant_id) VALUES ('widget-1', 1), ('widget-2', 1), ('other-client', 1), ('other-user', 1), ('non-widget', 1), ('other-tenant', 2)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO integration_chat_bindings (session_id, client_id, user_id, source) VALUES ('widget-1', 'client-1', 'user-1', 'widget'), ('widget-2', 'client-1', 'user-1', 'widget'), ('other-client', 'client-2', 'user-1', 'widget'), ('other-user', 'client-1', 'user-2', 'widget'), ('non-widget', 'client-1', 'user-1', ''), ('other-tenant', 'client-1', 'user-1', 'widget')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO messages (id, session_id, content, role, created_at) VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?), (?, ?, ?, ?, ?), (?, ?, ?, ?, ?), (?, ?, ?, ?, ?), (?, ?, ?, ?, ?), (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)`,
		"m1", "widget-1", "如何投标？", "user", now,
		"m2", "widget-2", "如何投标？", "user", now.Add(time.Second),
		"m3", "widget-1", "资质要求是什么？", "user", now.Add(2*time.Second),
		"m4", "other-client", "其他 client 问题", "user", now.Add(3*time.Second),
		"m5", "other-user", "其他用户问题", "user", now.Add(4*time.Second),
		"m6", "non-widget", "不应出现", "user", now.Add(5*time.Second),
		"m7", "other-tenant", "其他租户问题", "user", now.Add(6*time.Second),
		"m8", "widget-1", "忽略回答", "assistant", now.Add(7*time.Second),
	).Error)

	repo := NewMessageRepository(db)
	questions, err := repo.GetFrequentlyAskedQuestions(context.Background(), 1, "client-1", "user-1", 3)
	require.NoError(t, err)
	require.Equal(t, []string{"如何投标？", "资质要求是什么？"}, questions)
}
