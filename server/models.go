package main

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type User struct {
	bun.BaseModel `bun:"table:users"`

	ID                       uuid.UUID      `bun:"id,pk,type:uuid"`
	Username                 string         `bun:"username,notnull,unique"`
	Email                    string         `bun:"email,notnull,unique"`
	PublicKey                string         `bun:"public_key,notnull"`
	SigningPublicKey         string         `bun:"signing_public_key,notnull"`
	OpaqueRecord             string         `bun:"opaque_record,notnull"`
	WrappedX25519PrivateKey  string         `bun:"wrapped_x25519_private_key,notnull"`
	WrappedEd25519PrivateKey string         `bun:"wrapped_ed25519_private_key,notnull"`
	KeyWrapNonce             string         `bun:"key_wrap_nonce,notnull"`
	KeyWrapSalt              string         `bun:"key_wrap_salt,notnull"`
	KeyWrapParams            map[string]any `bun:"key_wrap_params,notnull,type:jsonb"`
	CreatedAt                time.Time      `bun:"created_at,notnull"`
	DeletedAt                *time.Time     `bun:"deleted_at,nullzero"`
}

type Friendship struct {
	bun.BaseModel `bun:"table:friendships"`

	ID          int64      `bun:"id,pk,autoincrement"`
	UserAID     uuid.UUID  `bun:"user_a_id,notnull,type:uuid"`
	UserBID     uuid.UUID  `bun:"user_b_id,notnull,type:uuid"`
	Status      string     `bun:"status,notnull,default:'pending'"`
	InitiatedBy uuid.UUID  `bun:"initiated_by,notnull,type:uuid"`
	CreatedAt   time.Time  `bun:"created_at,notnull"`
	RespondedAt *time.Time `bun:"responded_at,nullzero"`

	UserA     *User `bun:"rel:belongs-to,join:user_a_id=id"`
	UserB     *User `bun:"rel:belongs-to,join:user_b_id=id"`
	Initiator *User `bun:"rel:belongs-to,join:initiated_by=id"`
}

type Block struct {
	bun.BaseModel `bun:"table:blocks"`

	ID        int64     `bun:"id,pk,autoincrement"`
	BlockerID uuid.UUID `bun:"blocker_id,notnull,type:uuid"`
	BlockedID uuid.UUID `bun:"blocked_id,notnull,type:uuid"`
	CreatedAt time.Time `bun:"created_at,notnull"`

	Blocker *User `bun:"rel:belongs-to,join:blocker_id=id"`
	Blocked *User `bun:"rel:belongs-to,join:blocked_id=id"`
}

type Chat struct {
	bun.BaseModel `bun:"table:chats"`

	ID            uuid.UUID  `bun:"id,pk,type:uuid"`
	Type          string     `bun:"type,notnull"`
	CreatedBy     *uuid.UUID `bun:"created_by,type:uuid"`
	CurrentEpoch  int        `bun:"current_epoch,notnull,default:1"`
	DMPairKey     string     `bun:"dm_pair_key,unique"`
	EncryptedName *string    `bun:"encrypted_name,nullzero"`
	CreatedAt     time.Time  `bun:"created_at,notnull"`

	Creator *User `bun:"rel:belongs-to,join:created_by=id"`
}

type ChatMember struct {
	bun.BaseModel `bun:"table:chat_members"`

	ID       int64      `bun:"id,pk,autoincrement"`
	ChatID   uuid.UUID  `bun:"chat_id,notnull,type:uuid"`
	UserID   uuid.UUID  `bun:"user_id,notnull,type:uuid"`
	Role     string     `bun:"role,notnull,default:'member'"`
	JoinedAt time.Time  `bun:"joined_at,notnull"`
	LeftAt   *time.Time `bun:"left_at,nullzero"`

	Chat *Chat `bun:"rel:belongs-to,join:chat_id=id"`
	User *User `bun:"rel:belongs-to,join:user_id=id"`
}

type ChatEpochKey struct {
	bun.BaseModel `bun:"table:chat_epoch_keys"`

	ID         int64     `bun:"id,pk,autoincrement"`
	ChatID     uuid.UUID `bun:"chat_id,notnull,type:uuid"`
	Epoch      int       `bun:"epoch,notnull"`
	UserID     uuid.UUID `bun:"user_id,notnull,type:uuid"`
	WrappedKey string    `bun:"wrapped_key,notnull"`
	WrapNonce  string    `bun:"wrap_nonce,notnull"`
	CreatedAt  time.Time `bun:"created_at,notnull"`

	Chat *Chat `bun:"rel:belongs-to,join:chat_id=id"`
	User *User `bun:"rel:belongs-to,join:user_id=id"`
}

type Message struct {
	bun.BaseModel `bun:"table:messages"`

	ID              uuid.UUID `bun:"id,pk,type:uuid"`
	ChatID          uuid.UUID `bun:"chat_id,notnull,type:uuid"`
	Epoch           int       `bun:"epoch,notnull"`
	SenderID        uuid.UUID `bun:"sender_id,notnull,type:uuid"`
	Type            string    `bun:"type,notnull"`
	Ciphertext      string    `bun:"ciphertext,notnull"`
	Nonce           string    `bun:"nonce,notnull"`
	Signature       string    `bun:"signature,notnull"`
	ClientTimestamp time.Time `bun:"client_timestamp,notnull"`
	ServerSeq       int64     `bun:"server_seq,notnull"`
	CreatedAt       time.Time `bun:"created_at,notnull"`

	Chat   *Chat `bun:"rel:belongs-to,join:chat_id=id"`
	Sender *User `bun:"rel:belongs-to,join:sender_id=id"`
}

type AdminAuditLog struct {
	bun.BaseModel `bun:"table:admin_audit_log"`

	ID         int64      `bun:"id,pk,autoincrement"`
	AdminID    *uuid.UUID `bun:"admin_id,type:uuid"`
	Action     string     `bun:"action,notnull"`
	TargetType *string    `bun:"target_type,nullzero"`
	TargetID   *string    `bun:"target_id,nullzero"`
	CreatedAt  time.Time  `bun:"created_at,notnull"`

	Admin *User `bun:"rel:belongs-to,join:admin_id=id"`
}
