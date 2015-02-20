package libkb

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	jsonw "github.com/keybase/go-jsonw"
	triplesec "github.com/keybase/go-triplesec"
	keybase_1 "github.com/keybase/protocol/go"
)

type LoggedInResult struct {
	SessionId string
	CsrfToken string
	Uid       UID
	Username  string
}

type LoginState struct {
	Configured      bool
	LoggedIn        bool
	SessionVerified bool

	salt            []byte
	loginSession    []byte
	loginSessionB64 string
	tsec            *triplesec.Cipher
	tspkey          *TSPassKey
	sessionFor      string
	extraKeystream []byte

	loggedInRes *LoggedInResult
}

const (
	pwhIndex   = 0
	pwhLen     = 32
	eddsaIndex = pwhIndex + pwhLen
	eddsaLen   = 32
	dhIndex    = eddsaIndex + eddsaLen
	dhLen      = 32
	lksIndex   = dhIndex + dhLen
	lksLen     = 32
	extraLen   = pwhLen + eddsaLen + dhLen + lksLen
)

func NewLoginState() *LoginState {
	return &LoginState{}
}

func (s LoginState) GetCachedSharedSecret() []byte {
	return s.extraKeystream[pwhIndex:eddsaIndex]
}

func (s LoginState) GetCachedTSPassKey() TSPassKey {
	return TSPassKey(s.extraKeystream)
}

func (s LoginState) IsLoggedIn() bool {
	return s.LoggedIn
}

func (s *LoginState) GetSalt() (salt []byte, err error) {
	if s.salt == nil {
		s.salt = G.Env.GetSalt()
	}
	salt = s.salt
	return
}

func (s *LoginState) GenerateNewSalt() error {
	var err error
	s.salt, err = RandBytes(triplesec.SaltLen)
	if err != nil {
		return err
	}
	return nil
}

func (s *LoginState) GetSaltAndLoginSession(email_or_username string) error {

	if s.salt != nil && s.loginSession != nil && s.sessionFor == email_or_username {
		return nil
	}
	s.sessionFor = ""

	res, err := G.API.Get(ApiArg{
		Endpoint:    "getsalt",
		NeedSession: false,
		Args: HttpArgs{
			"email_or_username": S{email_or_username},
		},
	})
	if err != nil {
		return err
	}

	shex, err := res.Body.AtKey("salt").GetString()
	if err != nil {
		return err
	}

	s.salt, err = hex.DecodeString(shex)
	if err != nil {
		return err
	}

	ls_b64, err := res.Body.AtKey("login_session").GetString()
	if err != nil {
		return err
	}

	s.loginSession, err = base64.StdEncoding.DecodeString(ls_b64)
	if err != nil {
		return err
	}

	s.loginSessionB64 = ls_b64
	s.sessionFor = email_or_username

	return nil
}

func (s *LoginState) StretchKey(passphrase string) (err error) {
	if s.tsec == nil {
		if s.tsec, err = triplesec.NewCipher([]byte(passphrase), s.salt); err != nil {
			return err
		}
		_, s.extraKeystream, err = s.tsec.DeriveKey(extraLen)
	}
	return nil
}

func (s *LoginState) GetTSPassKey(passphrase string) (ret *TSPassKey, err error) {
	if err = s.StretchKey(passphrase) != nil {
		return
	}
	ret = s.GetCachedTSPassKey()
	return
}

func (s *LoginState) ComputeLoginPw() ([]byte, error) {
	mac := hmac.New(sha512.New, s.GetCachedSharedSecret())
	mac.Write(s.loginSession)
	return mac.Sum(nil), nil
}

func (s *LoginState) PostLoginToServer(eOu string, lgpw []byte) error {
	res, err := G.API.Post(ApiArg{
		Endpoint:    "login",
		NeedSession: false,
		Args: HttpArgs{
			"email_or_username": S{eOu},
			"hmac_pwh":          S{hex.EncodeToString(lgpw)},
			"login_session":     S{s.loginSessionB64},
		},
		AppStatus: []string{"OK", "BAD_LOGIN_PASSWORD"},
	})
	if err != nil {
		return err
	}
	if res.AppStatus == "BAD_LOGIN_PASSWORD" {
		err = PassphraseError{"server rejected login attempt"}
		return err
	}

	b := res.Body
	sessionId, err := b.AtKey("session").GetString()
	if err != nil {
		return err
	}
	csrfToken, err := b.AtKey("csrf_token").GetString()
	if err != nil {
		return err
	}
	uid, err := GetUid(b.AtKey("uid"))
	if err != nil {
		return nil
	}
	uname, err := b.AtKey("me").AtKey("basics").AtKey("username").GetString()
	if err != nil {
		return nil
	}

	s.loggedInRes = &LoggedInResult{sessionId, csrfToken, *uid, uname}
	return nil
}

func (s *LoginState) SaveLoginState(uidVerified bool) (err error) {
	s.LoggedIn = true
	s.SessionVerified = true
	s.loginSession = nil
	s.loginSessionB64 = ""

	if cfg := G.Env.GetConfigWriter(); cfg != nil {

		if err = cfg.SetUserConfig(NewUserConfig(s.loggedInRes.Uid, s.loggedInRes.Username,
			s.salt, nil), false); err != nil {
			return err
		}

		if err = cfg.Write(); err != nil {
			return err
		}
	}

	if sw := G.SessionWriter; sw != nil {
		sw.SetLoggedIn(*s.loggedInRes)
		if err = sw.Write(); err != nil {
			return err
		}
	}

	return nil
}

func (s *LoginState) Logout() error {
	G.Log.Debug("+ Logout called")
	err := G.Session.Logout()
	if err == nil {
		s.LoggedIn = false
		s.SessionVerified = false
		if s.tsec != nil {
			s.tsec.Scrub()
			s.tsec = nil
		}
		s.extraKeystream = nil
	}
	if G.SecretSyncer != nil {
		G.SecretSyncer.Clear()
	}
	G.Log.Debug("- Logout called")
	return err
}

type LoginArg struct {
	Force      bool
	Prompt     bool
	Retry      int
	RetryMsg   string
	Username   string
	Passphrase string
	Ui         LoginUI
	SecretUI   SecretUI
	NoUi       bool
}

func (r PostAuthProofRes) ToLoggedInResult() (ret *LoggedInResult, err error) {
	var uid *UID
	if uid, err = UidFromHex(r.UidHex); err != nil {
		return
	}
	ret = &LoggedInResult{
		SessionId: r.SessionId,
		CsrfToken: r.CsrfToken,
		Uid:       *uid,
		Username:  r.Username,
	}
	return
}

// PubkeyLogin looks for a locally available private key and tries
// to establish a session via public key signature.
func (s *LoginState) PubkeyLogin(name string, ui SecretUI) (err error) {
	var key GenericKey
	var me *User
	var proof *jsonw.Wrapper
	var sig string
	var pres *PostAuthProofRes
	var uc *UserConfig

	G.Log.Debug("+ PubkeyLogin()")
	defer func() { G.Log.Debug("- PubkeyLogin() -> %s", ErrToOk(err)) }()

	if len(name) == 0 {
		if uc, err = G.Env.GetConfig().GetUserConfig(); err != nil {
			G.Log.Debug("| Can't find current UserConfig")
		} else {
			name = uc.Name
		}
	} else if uc, err = G.Env.GetConfig().GetUserConfigForUsername(name); err != nil {
		G.Log.Debug("| No Userconfig for %s", name)
	}
	if err != nil {
		return
	}

	if me, err = LoadUser(LoadUserArg{Name: name}); err != nil {
		return
	}

	// Need the loginSession; the salt doesn't really matter here.
	if err = s.GetSaltAndLoginSession(name); err != nil {
		return
	}

	if key, err = G.Keyrings.GetSecretKey(SecretKeyArg{Reason: "login", Ui: ui, Me: me, All: true}); err != nil {
		return
	}

	if proof, err = me.AuthenticationProof(key, s.loginSessionB64, AUTH_EXPIRE_IN); err != nil {
		return
	}

	if sig, _, _, err = SignJson(proof, key); err != nil {
		return
	}

	arg := PostAuthProofArg{
		uid: me.id,
		sig: sig,
		key: key,
	}
	if pres, err = PostAuthProof(arg); err != nil {
		return
	}

	if s.loggedInRes, err = pres.ToLoggedInResult(); err != nil {
		return
	}

	err = s.SaveLoginState(false)

	return
}

func (s *LoginState) Login(arg LoginArg) (err error) {
	var loggedIn bool

	G.Log.Debug("+ Login called")
	defer func() { G.Log.Debug("- Login -> %s", ErrToOk(err)) }()

	if loggedIn, err = s.checkLoggedIn(arg); err != nil || loggedIn {
		return err
	}
	if err = s.setupUIs(&arg); err != nil {
		return err
	}
	if err = s.switchUser(arg); err != nil {
		return
	}

	if loggedIn, err = s.tryPubkeyLogin(arg); err != nil || loggedIn {
		return
	}
	if err = s.tryPasswordLogin(arg); err != nil {
		return err
	}
	return nil
}

func (s *LoginState) setupUIs(arg *LoginArg) (err error) {
	if arg.Ui == nil && !arg.NoUi {
		if G.UI != nil {
			arg.Ui = G.UI.GetLoginUI()
		} else {
			err = NoUiError{"login"}
			return
		}
	}
	if arg.SecretUI == nil && !arg.NoUi {
		if G.UI != nil {
			arg.SecretUI = G.UI.GetSecretUI()
		} else {
			err = NoUiError{"secret"}
			return
		}
	}
	return nil
}

func (s *LoginState) checkLoggedIn(arg LoginArg) (loggedIn bool, err error) {

	G.Log.Debug("+ checkedLoggedIn()")
	defer func() { G.Log.Debug("- checkedLoggedIn() -> %v,%s", loggedIn, ErrToOk(err)) }()

	var loggedInTmp bool
	if loggedInTmp, err = G.Session.LoadAndCheck(); err != nil {
		G.Log.Debug("| Session failed to load")
		return
	}

	un := G.Session.GetUsername()
	if loggedInTmp && len(arg.Username) > 0 && un != nil && arg.Username != *un {
		err = LoggedInError{}
		return
	}

	if !arg.Force && loggedInTmp {
		s.LoggedIn = true
		s.SessionVerified = true
		G.Log.Debug("| Our session token is still valid; we're logged in")
		loggedIn = true
	}
	return
}

func (s *LoginState) switchUser(arg LoginArg) error {
	if len(arg.Username) == 0 || !CheckUsername.F(arg.Username) {
	} else if err := G.Env.GetConfigWriter().SwitchUser(arg.Username); err != nil {
		G.Log.Debug("| Can't switch user to %s: %s", arg.Username, err.Error())
	} else {
		G.Log.Debug("| Successfully switched user to %s", arg.Username)
	}
	return nil
}

func (s *LoginState) tryPubkeyLogin(arg LoginArg) (loggedIn bool, err error) {
	if err = s.PubkeyLogin(arg.Username, arg.SecretUI); err == nil {
		G.Log.Debug("| Pubkey login succeeded")
		loggedIn = true
	} else if _, ok := err.(CanceledError); ok {
		G.Log.Debug("Canceled pubkey login, so cancel login")
	} else {
		G.Log.Debug("Public key login failed: %s", err.Error())
		G.Log.Debug("Falling back to passphrase login")
		err = nil
	}
	return
}

func (s *LoginState) tryPasswordLogin(arg LoginArg) (err error) {

	n_tries := arg.Retry
	if n_tries == 0 {
		n_tries = 1
	}

	for i := 0; i < n_tries; i++ {
		err = s.login(&arg)
		if err == nil {
			break
		} else if _, badpw := err.(PassphraseError); !badpw || len(arg.Passphrase) > 0 {
			break
		} else {
			arg.RetryMsg = err.Error()
		}
	}
	return
}

func (s *LoginState) getEmailOrUsername(arg *LoginArg) (res string, prompted bool, err error) {
	if res = arg.Username; len(res) != 0 {
	} else if res = G.Env.GetEmailOrUsername(); len(res) > 0 || !arg.Prompt || arg.Ui == nil {
	} else if res, err = arg.Ui.GetEmailOrUsername(0); err != nil {
	} else {
		arg.Username = res
		prompted = true
	}

	if len(res) == 0 {
		err = NoUsernameError{}
	}
	return
}

func (s *LoginState) login(arg *LoginArg) (err error) {
	G.Log.Debug("+ LoginState.login (username=%s)", arg.Username)
	defer func() {
		G.Log.Debug("- LoginState.login -> %s", ErrToOk(err))
	}()

	var emailOrUsername string
	var prompted bool

	if emailOrUsername, prompted, err = s.getEmailOrUsername(arg); err != nil {
		return
	}

	G.Log.Debug(fmt.Sprintf("| got username: %s\n", emailOrUsername))

	if err = s.GetSaltAndLoginSession(emailOrUsername); err != nil {
		return
	}

	if _, err = s.GetTriplesec(emailOrUsername, arg.Passphrase, arg.RetryMsg, arg.SecretUI); err != nil {
		return
	}

	var lgpw []byte
	lgpw, err = s.ComputeLoginPw()

	if err != nil {
		return
	}

	err = s.PostLoginToServer(emailOrUsername, lgpw)
	if err != nil {
		s.tsec = nil
		s.extraKeystream = nil
		return err
	}

	err = s.SaveLoginState(prompted)
	if err != nil {
		return err
	}

	return
}

func (s *LoginState) GetTriplesec(un string, pp string, retry string, ui SecretUI) (ret *triplesec.Cipher, err error) {
	if s.tsec != nil {
		ret = s.tsec
		return
	}
	var salt []byte
	if salt, err = s.GetSalt(); err != nil {
		return
	} else if salt == nil {
		err = fmt.Errorf("Cannot encrypt; no salt found")
	}

	arg := keybase_1.GetKeybasePassphraseArg{
		Username: un,
		Retry:    retry,
	}

	if len(pp) > 0 {
	} else if ui == nil {
		err = NoUiError{"secret"}
	} else if pp, err = ui.GetKeybasePassphrase(arg); err != nil {
		return
	}

	if err = s.StretchKey(pp); err != nil {
		return
	}

	ret = s.tsec
	return
}

func (s *LoginState) GetCachedTriplesec() *triplesec.Cipher {
	return s.tsec
}

//==================================================

type TSPassKey []byte

func (d TSPassKey) PWHash() []byte {
	return d[pwhIndex:eddsaIndex]
}

func (d TSPassKey) EdDSASeed() []byte {
	return d[eddsaIndex:dhIndex]
}

func (d TSPassKey) DHSeed() []byte {
	return d[dhIndex:lksIndex]
}

func (d TSPassKey) LksClientHalf() []byte {
	return d[lksIndex:]
}

func (d TSPassKey) String() string {
	return fmt.Sprintf("pwh:   %x\nEdDSA: %x\nDH:    %x\nlks:   %x", d.PWHash(), d.EdDSASeed(), d.DHSeed(), d.LksClientHalf())
}

//==================================================

