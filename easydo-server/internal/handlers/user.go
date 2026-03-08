package handlers

import (
	"easydo-server/internal/middleware"
	"easydo-server/internal/models"
	"net/http"
	"strconv"
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

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=6,max=32"`
	Email    string `json:"email"`
}

type UpdateProfileRequest struct {
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Bio      string `json:"bio"`
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

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"id":         user.ID,
			"username":   user.Username,
			"email":      user.Email,
			"phone":      user.Phone,
			"nickname":   user.Nickname,
			"avatar":     user.Avatar,
			"bio":        user.Bio,
			"role":       user.Role,
			"status":     user.Status,
			"created_at": user.CreatedAt,
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
