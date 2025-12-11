package models


// Match records a single gameplay session (user vs AI / PvP)
type Match struct {
	ID              string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ExternalUserID  string    `gorm:"index;not null" json:"external_user_id"`
	GameID          string    `gorm:"index;not null" json:"game_id"`
	TournamentID    *string   `gorm:"index" json:"tournament_id,omitempty"` // nil = casual match

	// Game outcome
	Score      int64  `json:"score"`
	Result     string `json:"result" gorm:"type:varchar(16);check:result IN ('win','loss','draw','incomplete')"` // win/loss/draw
	DurationSec int    `json:"duration_sec" gorm:"default:0"`

	// XP awarded (pre-calculated to avoid recomputation)
	XPEarned int64 `json:"xp_earned" gorm:"default:0"`

	Timestamps
}