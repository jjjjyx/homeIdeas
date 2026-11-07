package domain

// 家庭角色
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// 账目可见性
const (
	VisibilityFamily  = "family"
	VisibilityPrivate = "private"
)

// 开支归属：这笔钱算家庭共同，还是某个人的个人消费
const (
	ScopeFamily   = "family"
	ScopePersonal = "personal"
	ScopeUnknown  = "unknown"
	// ScopeTransfer 资金往来：转账给个人、收款码收款等，本质上不是消费，
	// 混进支出统计会严重失真（实测占全年 35%）
	ScopeTransfer = "transfer"
)

// 分类来源
const (
	CategorySourceAI     = "ai"
	CategorySourceRule   = "rule"
	CategorySourceManual = "manual"
)

// 记账类型，与 App 端保持一致
const (
	TypeExpense = 0
	TypeIncome  = 1
)

type User struct {
	ID           string
	Account      string
	PasswordHash string
	Nickname     string
	AvatarURL    string
	Phone        string
	Status       int // 1 正常 0 禁用
	CreatedAt    int64
	UpdatedAt    int64
}

// Family 系统唯一的家庭。单家庭模式下只存在一行有效记录。
type Family struct {
	ID        string
	Name      string
	OwnerID   string
	Currency  string
	Rev       int64
	CreatedAt int64
	UpdatedAt int64
}

type Member struct {
	FamilyID string
	UserID   string
	Role     string
	JoinedAt int64
	// 冗余的用户信息，便于列表直接展示
	Nickname string
	Account  string
}

// Invite 加入唯一家庭的邀请码。注册时凭码加入，角色由邀请码决定。
type Invite struct {
	ID        string
	FamilyID  string
	Code      string
	Role      string
	CreatedBy string
	ExpiresAt *int64
	UsedBy    string
	UsedAt    *int64
	CreatedAt int64
}

type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	Device    string
	DeviceID  string
	ExpiresAt int64
	RevokedAt *int64
	CreatedAt int64
}

type AuditLog struct {
	FamilyID   string
	UserID     string
	Action     string
	TargetType string
	TargetID   string
	Detail     string
	IP         string
}

// Transaction 一笔账。金额单位为「分」，amount 永远为正，方向由 Type 决定。
type Transaction struct {
	ID          string
	FamilyID    string
	Type        int
	AmountCents int64
	CategoryID  string
	AccountID   string
	MemberID    string // 记账人
	OccurredAt  int64  // 账目发生时间（毫秒）
	Note        string
	Visibility  string // family / private
	Tags        string
	Rev         int64
	CreatedAt   int64
	UpdatedAt   int64
	DeletedAt   *int64
	// 来源：manual 手工记 / wechat / alipay / unionpay / bank（导入）
	Source string
	// 外部交易单号，用于账单导入去重
	ExternalID string

	// Scope 开支归属：family 家庭共同 / personal 个人 / unknown 待判断
	Scope string
	// OwnerID 个人开支的归属人（scope=personal 时有效）
	OwnerID string
	// CategorySource 分类来源：ai / rule / manual / ""
	CategorySource string
	AIConfidence   float64
	AIReason       string
	// Reviewed 是否已人工确认
	Reviewed bool
	// ParentID 拆单后指向原账目
	ParentID string
	// IsSplit 这条是「已拆分的父账目」，只留档，不进统计
	IsSplit bool
}

type Account struct {
	ID        string
	FamilyID  string
	Name      string
	Kind      string
	SortOrder int
	Rev       int64
	CreatedAt int64
	UpdatedAt int64
	DeletedAt *int64
}

// 设备配对状态
const (
	DevicePending  = "pending"
	DeviceApproved = "approved"
	DeviceRejected = "rejected"
	DeviceExpired  = "expired"
	DeviceRevoked  = "revoked"
)

// Device 一台待授权或已授权的设备
type Device struct {
	ID         string
	DeviceUID  string
	DeviceKey  string
	UserID     string
	FamilyID   string
	Role       string
	Model      string
	OSVersion  string
	AppVersion string
	IP         string
	Status     string
	PairCode   string
	CreatedAt  int64
	ExpiresAt  int64
	ApprovedAt *int64
	ApprovedBy string
	LastSeenAt *int64
}
