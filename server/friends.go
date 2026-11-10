package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type friendsHandler struct {
	db *bun.DB
}

type sendFriendRequest struct {
	RecipientUsername string `json:"recipient_username"`
}

type friendRequestResponse struct {
	Action string `json:"action" binding:"required,oneof=accept decline"`
}

func newFriendsHandler(db *bun.DB) *friendsHandler {
	return &friendsHandler{db: db}
}

func sortUUIDs(u1, u2 uuid.UUID) (uuid.UUID, uuid.UUID) {
	if strings.Compare(u1.String(), u2.String()) < 0 {
		return u1, u2
	}
	return u2, u1
}

// POST /api/friends/request
func (h *friendsHandler) sendRequest(c *gin.Context) {
	userId := c.MustGet("user_id").(uuid.UUID)
	var input sendFriendRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}
	// Debug log to terminal
	fmt.Printf("DEBUG INPUT -> Raw JSON bind result: %+v (Length: %d)\n", input, len(input.RecipientUsername))

	targetUsername := strings.ToLower(strings.TrimSpace(input.RecipientUsername))
	if targetUsername == "" {
		respondError(c, http.StatusBadRequest, "invalid target username")
		return
	}

	ctx := c.Request.Context()
	var recipient User
	if err := h.db.NewSelect().Model(&recipient).Where("username = ? AND deleted_at IS NULL", targetUsername).Scan(ctx); err != nil {
		respondError(c, http.StatusNotFound, "user not found")
		return
	}

	if recipient.ID == userId {
		respondError(c, http.StatusBadRequest, "cannot send friend request to yourself")
		return
	}

	// if the user is blocked, you can not send a friend request
	blockedCount, err := h.db.NewSelect().Model((*Block)(nil)).
		Where("(blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)",
			userId, recipient.ID, recipient.ID, userId).
		Count(ctx)
	if err != nil || blockedCount > 0 {
		respondError(c, http.StatusForbidden, "cannot interact with this user")
		return
	}

	userA, userB := sortUUIDs(userId, recipient.ID)

	var existing Friendship
	err = h.db.NewSelect().Model(&existing).
		Where("user_a_id = ? AND user_b_id = ?", userA, userB).
		Scan(ctx)

	if err == nil {
		if existing.Status == "accepted" {
			respondError(c, http.StatusConflict, "already friends")
			return
		}
		if existing.Status == "pending" {
			respondError(c, http.StatusConflict, "a friend request already exists between these users")
			return
		}
		// If previously declined, allow re-initiating by updating the record
		existing.Status = "pending"
		existing.InitiatedBy = userId
		existing.CreatedAt = time.Now().UTC()
		existing.RespondedAt = nil
		_, err = h.db.NewUpdate().Model(&existing).WherePK().Exec(ctx)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "could not update friend request")
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "friend request sent successfully"})
		return
	}
	friendship := &Friendship{
		UserAID:     userA,
		UserBID:     userB,
		Status:      "pending",
		InitiatedBy: userId,
		CreatedAt:   time.Now().UTC(),
	}

	if _, err := h.db.NewInsert().Model(friendship).Exec(ctx); err != nil {
		respondError(c, http.StatusInternalServerError, "could not send friend request")
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "friend request sent successfully"})
}

// GET /api/friends/requests
func (h *friendsHandler) listRequests(c *gin.Context) {
	userId := c.MustGet("user_id").(uuid.UUID)
	ctx := c.Request.Context()

	var friendships []Friendship
	err := h.db.NewSelect().Model(&friendships).
		Relation("UserA").
		Relation("UserB").
		Relation("Initiator").
		Where("(user_a_id = ? OR user_b_id = ?) AND status = ? AND initiated_by != ?", userId, userId, "pending", userId).
		Scan(ctx)

	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not fetch friend requests")
		return
	}
	type RequestDTO struct {
		ID         int64     `json:"id"`
		FromUser   string    `json:"from_username"`
		FromID     string    `json:"from_user_id"`
		CreatedAt  time.Time `json:"created_at"`
		IsIncoming bool      `json:"is_incoming"`
	}

	var results []RequestDTO
	for _, f := range friendships {
		var fromUser *User
		isIncoming := f.InitiatedBy != userId
		if f.InitiatedBy == f.UserAID {
			fromUser = f.UserA
		} else {
			fromUser = f.UserB
		}

		results = append(results, RequestDTO{
			ID:         f.ID,
			FromUser:   fromUser.Username,
			FromID:     fromUser.ID.String(),
			CreatedAt:  f.CreatedAt,
			IsIncoming: isIncoming,
		})
	}

	c.JSON(http.StatusOK, gin.H{"requests": results})
}

// PUT /api/friends/requests/:id
func (h *friendsHandler) respondRequest(c *gin.Context) {
	userId := c.MustGet("user_id").(uuid.UUID)
	requestId := c.Param("id")

	var input friendRequestResponse
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid request body")
		return
	}
	ctx := c.Request.Context()
	var friendship Friendship
	err := h.db.NewSelect().Model(&friendship).Where("id = ? AND status = ?", requestId, "pending").
		Scan(ctx)

	if err != nil {
		respondError(c, http.StatusNotFound, "pending request not found")
		return
	}
	if (friendship.UserAID != userId && friendship.UserBID != userId) || friendship.InitiatedBy == userId {
		respondError(c, http.StatusForbidden, "not authorized to respond to this request")
		return
	}
	now := time.Now().UTC()
	if input.Action == "accept" {
		friendship.Status = "accepted"
		friendship.RespondedAt = &now
	} else {
		friendship.Status = "declined"
		friendship.RespondedAt = &now
	}
	_, err = h.db.NewUpdate().Model(&friendship).Column("status", "responded_at").Where("id  = ?", friendship.ID).Exec(ctx)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "updating friend request status failed")
		return
	}

	c.JSON(http.StatusOK, gin.H{"Status": friendship.Status})
}

// GET /api/friends
func (h *friendsHandler) getFriends(c *gin.Context) {
	userId := c.MustGet("user_id").(uuid.UUID)
	ctx := c.Request.Context()

	var friendships []Friendship
	err := h.db.NewSelect().Model(&friendships).Relation("UserA").Relation("UserB").
		Where("(user_a_id=? OR user_b_id=?) AND status=?", userId, userId, "accepted").Scan(ctx)

	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not fetch friends list")
		return
	}
	type FriendDTO struct {
		ID        string    `json:"id"`
		Username  string    `json:"username"`
		PublicKey string    `json:"public_key"`
		Since     time.Time `json:"since"`
	}
	var friends []FriendDTO
	for _, f := range friendships {
		var friend *User
		if f.UserAID == userId {
			friend = f.UserB
		} else {
			friend = f.UserA
		}

		friends = append(friends, FriendDTO{
			ID:        friend.ID.String(),
			Username:  friend.Username,
			PublicKey: friend.PublicKey, // Needed for future E2EE session setup!
			Since:     *f.RespondedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"friends": friends})
}
