package response

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
	// ConfirmPassword must repeat NewPassword.
	ConfirmPassword string `json:"confirm_password"`
}
