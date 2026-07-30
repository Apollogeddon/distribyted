package auth

import "crypto/subtle"

// CredentialsMatch reports whether the presented user/pass match the configured
// ones. It is constant-time w.r.t. the credential contents and always false when
// the configured credentials are empty, so an unset config can never authorize.
func CredentialsMatch(gotUser, gotPass, wantUser, wantPass string) bool {
	if wantUser == "" || wantPass == "" {
		return false
	}
	u := subtle.ConstantTimeCompare([]byte(gotUser), []byte(wantUser))
	p := subtle.ConstantTimeCompare([]byte(gotPass), []byte(wantPass))
	// & instead of && : short-circuiting here would leak via timing which
	// credential failed.
	return u&p == 1
}
