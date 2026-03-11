package handlers

import (
	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type UserHandler struct {
	DB *gorm.DB
}

func NewUserHandler() *UserHandler {
	return &UserHandler{DB: models.DB}
}

func sanitizeWorkspaceSlug(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	input = strings.ReplaceAll(input, " ", "-")
	input = strings.ReplaceAll(input, "_", "-")
	input = strings.Trim(input, "-")
	if input == "" {
		return "workspace"
	}
	return input
}

func (h *UserHandler) ensurePersonalWorkspace(user *models.User) (*models.Workspace, error) {
	return ensurePersonalWorkspaceWithDB(h.DB, user)
}

func ensurePersonalWorkspaceWithDB(db *gorm.DB, user *models.User) (*models.Workspace, error) {
	if user == nil {
		return nil, fmt.Errorf("user is required")
	}
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}

	var member models.WorkspaceMember
	if err := db.Model(&models.WorkspaceMember{}).
		Joins("JOIN workspaces ON workspaces.id = workspace_members.workspace_id").
		Where("workspace_members.user_id = ? AND workspace_members.status = ?", user.ID, models.WorkspaceMemberStatusActive).
		Where("workspaces.status = ?", models.WorkspaceStatusActive).
		Order("workspace_members.created_at ASC").
		First(&member).Error; err == nil {
		var workspace models.Workspace
		if err := db.Where("id = ? AND status = ?", member.WorkspaceID, models.WorkspaceStatusActive).First(&workspace).Error; err == nil {
			return &workspace, nil
		}
	}

	baseSlug := sanitizeWorkspaceSlug(user.Username)
	workspace := &models.Workspace{
		Name:       fmt.Sprintf("%s Workspace", user.Username),
		Slug:       fmt.Sprintf("%s-%d", baseSlug, user.ID),
		Status:     models.WorkspaceStatusActive,
		Visibility: models.WorkspaceVisibilityPrivate,
		CreatedBy:  user.ID,
	}
	if err := db.Create(workspace).Error; err != nil {
		return nil, err
	}
	member = models.WorkspaceMember{
		WorkspaceID: workspace.ID,
		UserID:      user.ID,
		Role:        models.WorkspaceRoleOwner,
		Status:      models.WorkspaceMemberStatusActive,
		InvitedBy:   user.ID,
		JoinedAt:    time.Now().Unix(),
	}
	if err := db.Create(&member).Error; err != nil {
		return nil, err
	}
	return workspace, nil
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=6,max=32"`
	Email    string `json:"email"`
}

type CreateUserRequest struct {
	Username      string `json:"username" binding:"required,min=3,max=32"`
	Password      string `json:"password" binding:"required,min=6,max=64"`
	Email         string `json:"email"`
	Nickname      string `json:"nickname"`
	SystemRole    string `json:"system_role"`
	WorkspaceID   uint64 `json:"workspace_id"`
	WorkspaceRole string `json:"workspace_role"`
}

type UpdateProfileRequest struct {
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Bio      string `json:"bio"`
}

func normalizeSystemRole(role string) string {
	if strings.EqualFold(strings.TrimSpace(role), "admin") {
		return "admin"
	}
	return "user"
}

func isValidSystemRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "admin":
		return true
	default:
		return false
	}
}

func isValidWorkspaceRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case models.WorkspaceRoleViewer, models.WorkspaceRoleDeveloper, models.WorkspaceRoleMaintainer, models.WorkspaceRoleOwner:
		return true
	default:
		return false
	}
}

func (h *UserHandler) createUserWithWorkspaceBinding(tx *gorm.DB, req CreateUserRequest, actorID uint64, targetSystemRole string, targetWorkspaceID uint64, targetWorkspaceRole string) (*models.User, error) {
	user := &models.User{
		Username: req.Username,
		Email:    strings.TrimSpace(req.Email),
		Nickname: strings.TrimSpace(req.Nickname),
		Role:     targetSystemRole,
		Status:   "active",
	}
	if err := user.SetPassword(req.Password); err != nil {
		return nil, err
	}
	if err := tx.Create(user).Error; err != nil {
		return nil, err
	}
	if _, err := ensurePersonalWorkspaceWithDB(tx, user); err != nil {
		return nil, err
	}
	if targetWorkspaceID > 0 {
		member := models.WorkspaceMember{
			WorkspaceID: targetWorkspaceID,
			UserID:      user.ID,
			Role:        targetWorkspaceRole,
			Status:      models.WorkspaceMemberStatusActive,
			InvitedBy:   actorID,
			JoinedAt:    time.Now().Unix(),
		}
		if err := tx.Create(&member).Error; err != nil {
			return nil, err
		}
	}
	return user, nil
}

func (h *UserHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误",
		})
		return
	}

	var user models.User
	result := h.DB.Where("username = ?", req.Username).First(&user)
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户名或密码错误",
		})
		return
	}

	if !user.CheckPassword(req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "用户名或密码错误",
		})
		return
	}

	if user.Status != "active" {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "账户已被禁用",
		})
		return
	}

	token, expiresAt, err := middleware.IssueTokenSession(c.Request.Context(), &user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "生成 token 失败",
		})
		return
	}

	user.LastLoginAt = time.Now().Unix()
	h.DB.Model(&user).Update("last_login_at", user.LastLoginAt)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"token":            token,
			"expires_at":       expiresAt,
			"expires_in":       int64(middleware.GetAuthTokenTTL().Seconds()),
			"refresh_interval": int64(middleware.GetAuthRefreshInterval().Seconds()),
			"user": gin.H{
				"id":       user.ID,
				"username": user.Username,
				"email":    user.Email,
				"nickname": user.Nickname,
				"avatar":   user.Avatar,
				"role":     user.Role,
			},
		},
	})
}

func (h *UserHandler) RefreshToken(c *gin.Context) {
	token := ""
	if v, ok := c.Get("auth_token"); ok {
		token, _ = v.(string)
	}
	if token == "" {
		var err error
		token, err = middleware.ExtractBearerToken(c.GetHeader("Authorization"))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "登录已过期",
			})
			return
		}
	}

	expiresAt, err := middleware.RefreshTokenSession(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "登录已过期",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"token":            token,
			"expires_at":       expiresAt,
			"expires_in":       int64(middleware.GetAuthTokenTTL().Seconds()),
			"refresh_interval": int64(middleware.GetAuthRefreshInterval().Seconds()),
		},
	})
}

func (h *UserHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误",
		})
		return
	}

	var existUser models.User
	if h.DB.Where("username = ?", req.Username).First(&existUser).Error == nil {
		c.JSON(http.StatusConflict, gin.H{
			"code":    409,
			"message": "用户名已存在",
		})
		return
	}

	user := &models.User{
		Username: req.Username,
		Email:    req.Email,
		Role:     "user",
		Status:   "active",
	}

	if err := user.SetPassword(req.Password); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码加密失败",
		})
		return
	}

	if err := h.DB.Create(user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建用户失败",
		})
		return
	}

	if _, err := h.ensurePersonalWorkspace(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建默认工作空间失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "注册成功",
	})
}

func (h *UserHandler) GetUserInfo(c *gin.Context) {
	userID := c.GetUint64("user_id")

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	if _, err := h.ensurePersonalWorkspace(&user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "加载默认工作空间失败",
		})
		return
	}

	var memberships []models.WorkspaceMember
	requestedWorkspaceID := c.GetUint64("workspace_id")
	currentWorkspace := gin.H{}
	permissions := []string{}
	workspaces := []gin.H{}

	if isAdminRole(user.Role) {
		var workspaceModels []models.Workspace
		if err := h.DB.Where("status = ?", models.WorkspaceStatusActive).Order("created_at ASC").Find(&workspaceModels).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "加载工作空间失败",
			})
			return
		}
		ownerCapabilities := middleware.ExpandWorkspaceCapabilities(models.WorkspaceRoleOwner)
		workspaces = make([]gin.H, 0, len(workspaceModels))
		for i := range workspaceModels {
			workspace := workspaceModels[i]
			entry := gin.H{
				"id":           workspace.ID,
				"name":         workspace.Name,
				"slug":         workspace.Slug,
				"role":         models.WorkspaceRoleOwner,
				"status":       workspace.Status,
				"visibility":   workspace.Visibility,
				"description":  workspace.Description,
				"capabilities": ownerCapabilities,
			}
			workspaces = append(workspaces, entry)
			if requestedWorkspaceID == 0 || workspace.ID == requestedWorkspaceID {
				if len(currentWorkspace) == 0 || workspace.ID == requestedWorkspaceID {
					currentWorkspace = entry
					permissions = ownerCapabilities
				}
			}
		}
	} else {
		if err := h.DB.Preload("Workspace", "status = ?", models.WorkspaceStatusActive).Where(
			"workspace_members.user_id = ? AND workspace_members.status = ?",
			user.ID,
			models.WorkspaceMemberStatusActive,
		).Joins("JOIN workspaces ON workspaces.id = workspace_members.workspace_id").Where("workspaces.status = ?", models.WorkspaceStatusActive).Order("workspace_members.created_at ASC").Find(&memberships).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"message": "加载工作空间失败",
			})
			return
		}
		workspaces = make([]gin.H, 0, len(memberships))
		for i := range memberships {
			membership := memberships[i]
			if membership.Workspace == nil {
				continue
			}
			entry := gin.H{
				"id":           membership.Workspace.ID,
				"name":         membership.Workspace.Name,
				"slug":         membership.Workspace.Slug,
				"role":         models.NormalizeWorkspaceRole(membership.Role),
				"status":       membership.Workspace.Status,
				"visibility":   membership.Workspace.Visibility,
				"description":  membership.Workspace.Description,
				"capabilities": middleware.ExpandWorkspaceCapabilities(membership.Role),
			}
			workspaces = append(workspaces, entry)
			if requestedWorkspaceID == 0 || membership.Workspace.ID == requestedWorkspaceID {
				if len(currentWorkspace) == 0 || membership.Workspace.ID == requestedWorkspaceID {
					currentWorkspace = entry
					permissions = middleware.ExpandWorkspaceCapabilities(membership.Role)
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"id":                user.ID,
			"username":          user.Username,
			"email":             user.Email,
			"phone":             user.Phone,
			"nickname":          user.Nickname,
			"avatar":            user.Avatar,
			"bio":               user.Bio,
			"role":              user.Role,
			"status":            user.Status,
			"permissions":       permissions,
			"workspaces":        workspaces,
			"current_workspace": currentWorkspace,
			"created_at":        user.CreatedAt,
		},
	})
}

func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userID := c.GetUint64("user_id")
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误",
		})
		return
	}

	updates := gin.H{}
	if req.Nickname != "" {
		updates["nickname"] = req.Nickname
	}
	if req.Email != "" {
		updates["email"] = req.Email
	}
	if req.Phone != "" {
		updates["phone"] = req.Phone
	}
	if req.Bio != "" {
		updates["bio"] = req.Bio
	}

	if err := h.DB.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
	})
}

func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID := c.GetUint64("user_id")

	var req struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误",
		})
		return
	}

	var user models.User
	if err := h.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "用户不存在",
		})
		return
	}

	if !user.CheckPassword(req.CurrentPassword) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "当前密码错误",
		})
		return
	}

	if err := user.SetPassword(req.NewPassword); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "密码加密失败",
		})
		return
	}

	h.DB.Model(&user).Update("password", user.Password)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "密码修改成功",
	})
}

func (h *UserHandler) Logout(c *gin.Context) {
	if session, ok := c.Get("session_id"); ok {
		sessionID, _ := session.(string)
		if sessionID != "" {
			if err := middleware.RevokeSessionByID(c.Request.Context(), sessionID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code":    500,
					"message": "退出失败",
				})
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "退出成功",
	})
}

func (h *UserHandler) GetUserList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	var users []models.User
	var total int64

	offset := (page - 1) * pageSize
	h.DB.Model(&models.User{}).Count(&total)

	if err := h.DB.Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取用户列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"list":  users,
			"total": total,
			"page":  page,
			"size":  pageSize,
		},
	})
}
