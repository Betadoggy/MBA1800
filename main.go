package main

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	mrand "math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type Problem struct {
	ID           int               `json:"id"`
	Category     string            `json:"category"`
	Question     string            `json:"question"`
	BoxContent   string            `json:"box_content"`
	TableContent string            `json:"table_content"`
	ImageURL     string            `json:"image_url"`
	HasImage     bool              `json:"has_image"`
	Options      map[string]string `json:"options"`
	Answer       int               `json:"answer"`
	Explanation  string            `json:"explanation"`
}

type CategoryGroup struct {
	Name          string
	Subcategories []string
}

type CategoryStat struct {
	Name         string
	WrongCount   int
	ProblemCount int
}

type WeaknessGroup struct {
	Name          string
	WrongCount    int
	ChartWidth    int
	Subcategories []CategoryStat
}

type User struct {
	ID           int       `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type RenderData struct {
	Problem
	TableContent      template.HTML
	IsBookmarked      bool
	CategoryGroups    []CategoryGroup
	SelectedCategory  string
	UserID            int
	UserEmail         string
	AccountInitial    string
	IsAuthenticated   bool
	ShowBookmarksOnly bool
}

type SubmitRequest struct {
	UserID         int `json:"user_id"`
	ProblemID      int `json:"problem_id"`
	SelectedOption int `json:"selected_option"`
	TimeSpent      int `json:"time_spent"`
}

type BookmarkRequest struct {
	ProblemID int  `json:"problem_id"`
	Bookmark  bool `json:"bookmark"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type SignupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type Progress struct {
	UserID       int       `json:"user_id"`
	ProblemID    int       `json:"problem_id"`
	IntervalDays int       `json:"interval_days"`
	EaseFactor   float64   `json:"ease_factor"`
	Repetitions  int       `json:"repetitions"`
	NextReview   time.Time `json:"next_review"`
	IsBookmarked bool      `json:"is_bookmarked"`
	WrongCount   int       `json:"wrong_count"`
}

type AuthPageData struct {
	Mode      string
	Error     string
	UserEmail string
}

type FrequentWrongProblem struct {
	ID           int
	Category     string
	Question     string
	WrongCount   int
	ProblemCount int
}

type WeaknessesPageData struct {
	Groups []WeaknessGroup
}

type FrequentWrongPageData struct {
	Problems []FrequentWrongProblem
}

var (
	globalProblems        []Problem
	problemMap            map[int]Problem
	quizTemplate          *template.Template
	authTemplate          *template.Template
	weaknessesTemplate    *template.Template
	frequentWrongTemplate *template.Template
	db                    *sql.DB
	categoryGroups        = []CategoryGroup{
		{
			Name: "Part 01 인사/조직/전략",
			Subcategories: []string{
				"경영일반",
				"조직행동 : 개인",
				"조직행동 : 집단과 조직",
				"조직이론",
				"인적자원관리",
				"전략경영",
				"국제경영",
			},
		},
		{
			Name: "Part 02 마케팅",
			Subcategories: []string{
				"마케팅 개요",
				"마케팅 조사",
				"마케팅 전략",
				"제품, 서비스, 브랜드",
				"가격",
				"유통",
				"촉진",
				"소비자 행동",
			},
		},
		{
			Name: "Part 03 경영과학/운영관리",
			Subcategories: []string{
				"경영과학",
				"생산시스템과 프로세스 관리",
				"품질경영",
				"생산능력 관리",
				"공급사슬 관리",
				"재고관리",
				"운영계획과 자원계획",
				"린 시스템 설계",
				"경영정보시스템",
			},
		},
		{
			Name: "Part 04 회계",
			Subcategories: []string{
				"회계의 기초",
				"회계처리와 CVP 분석",
				"회계정보의 이용",
			},
		},
		{
			Name: "Part 05 재무관리",
			Subcategories: []string{
				"재무관리의 기초",
				"위험과 수익률",
				"자본시장과 증권평가",
				"자본비용과 가치평가",
				"파생상품",
				"국제재무관리와 재무비율분석",
			},
		},
	}
)

func main() {
	mrand.Seed(time.Now().UnixNano())

	if err := loadProblems("data.json"); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	problemMap = buildProblemMap(globalProblems)
	fmt.Printf("성공적으로 %d개의 문제를 로드했습니다.\n", len(globalProblems))

	quizTemplate = template.Must(template.ParseFiles("templates/quiz.html"))
	authTemplate = template.Must(template.ParseFiles("templates/auth.html"))
	weaknessesTemplate = template.Must(template.ParseFiles("templates/weaknesses.html"))
	frequentWrongTemplate = template.Must(template.ParseFiles("templates/frequent_wrong.html"))

	var err error
	db, err = openDB(getDatabaseURL())
	if err != nil {
		panic(err)
	}
	defer db.Close()

	if err := ensureSchema(); err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleQuiz)
	mux.HandleFunc("/login", handleLogin)
	mux.HandleFunc("/signup", handleSignup)
	mux.HandleFunc("/logout", handleLogout)
	mux.HandleFunc("/weaknesses", handleWeaknesses)
	mux.HandleFunc("/frequent-wrong", handleFrequentWrong)
	mux.HandleFunc("/api/submit", handleSubmit)
	mux.HandleFunc("/api/bookmark", handleBookmark)

	port := getPort()
	fmt.Printf("DB 연동 서버 가동: http://localhost:%s\n", port)
	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}

func loadProblems(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("데이터 파일 로드 실패: %w", err)
	}

	if err := json.Unmarshal(data, &globalProblems); err != nil {
		return fmt.Errorf("데이터 파싱 실패: %w", err)
	}

	return nil
}

func buildProblemMap(problems []Problem) map[int]Problem {
	m := make(map[int]Problem, len(problems))
	for _, p := range problems {
		m[p.ID] = p
	}
	return m
}

func filterProblemsByIDs(problems []Problem, ids []int) []Problem {
	if len(ids) == 0 {
		return nil
	}

	selected := make([]Problem, 0, len(ids))
	idSet := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	for _, p := range problems {
		if _, ok := idSet[p.ID]; ok {
			selected = append(selected, p)
		}
	}
	return selected
}

func getCategories(problems []Problem) []string {
	seen := make(map[string]struct{})
	categories := make([]string, 0, len(problems))
	for _, p := range problems {
		if _, ok := seen[p.Category]; ok {
			continue
		}
		seen[p.Category] = struct{}{}
		categories = append(categories, p.Category)
	}
	return categories
}

func getAccountInitial(email string) string {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return "U"
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		trimmed = trimmed[:at]
	}
	if trimmed == "" {
		return "U"
	}
	return strings.ToUpper(string(trimmed[0]))
}

func getDatabaseURL() string {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgresql://postgres.slqdijguwqccgdkbillw:Mgs%5E98092222@aws-1-ap-northeast-2.pooler.supabase.com:6543/postgres"
	}
	return url
}

func getPort() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return port
}

func openDB(url string) (*sql.DB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func ensureSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id serial PRIMARY KEY,
			email text UNIQUE NOT NULL,
			password_hash text NOT NULL,
			created_at timestamptz DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS user_progress (
			id serial PRIMARY KEY,
			user_id integer NOT NULL DEFAULT 0,
			problem_id integer NOT NULL,
			interval_days integer NOT NULL DEFAULT 0,
			ease_factor double precision NOT NULL DEFAULT 2.5,
			repetitions integer NOT NULL DEFAULT 0,
			next_review timestamptz NOT NULL DEFAULT NOW(),
			is_bookmarked boolean NOT NULL DEFAULT false,
			wrong_count integer NOT NULL DEFAULT 0,
			created_at timestamptz DEFAULT NOW(),
			updated_at timestamptz DEFAULT NOW()
		)`,
	}

	for _, query := range queries {
		if _, err := db.ExecContext(context.Background(), query); err != nil {
			return err
		}
	}

	var hasUserID bool
	err := db.QueryRowContext(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'user_progress' AND column_name = 'user_id'
		)
	`).Scan(&hasUserID)
	if err != nil {
		return err
	}
	if !hasUserID {
		_, err = db.ExecContext(context.Background(), `ALTER TABLE user_progress ADD COLUMN IF NOT EXISTS user_id integer NOT NULL DEFAULT 0`)
		if err != nil {
			return err
		}
	}

	var hasWrongCount bool
	err = db.QueryRowContext(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'user_progress' AND column_name = 'wrong_count'
		)
	`).Scan(&hasWrongCount)
	if err != nil {
		return err
	}
	if !hasWrongCount {
		_, err = db.ExecContext(context.Background(), `ALTER TABLE user_progress ADD COLUMN IF NOT EXISTS wrong_count integer NOT NULL DEFAULT 0`)
		if err != nil {
			return err
		}
	}

	return nil
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 8)
	if _, err := crand.Read(salt); err != nil {
		return "", err
	}
	sum := sha256.Sum256(append(salt, []byte(password)...))
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(sum[:]), nil
}

func checkPasswordHash(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, ":")
	if len(parts) != 2 {
		return false
	}

	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}

	sum := sha256.Sum256(append(salt, []byte(password)...))
	return hex.EncodeToString(sum[:]) == parts[1]
}

func createUser(email, password string) (User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}

	var user User
	err = db.QueryRowContext(context.Background(), `
		INSERT INTO users (email, password_hash)
		VALUES ($1, $2)
		RETURNING id, email, password_hash, created_at
	`, email, hash).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt)
	return user, err
}

func getUserByEmail(email string) (User, error) {
	var user User
	err := db.QueryRowContext(context.Background(), `
		SELECT id, email, password_hash, created_at
		FROM users
		WHERE email = $1
	`, strings.ToLower(strings.TrimSpace(email))).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err == sql.ErrNoRows {
		return User{}, sql.ErrNoRows
	}
	return user, err
}

func getUserByID(id int) (User, error) {
	var user User
	err := db.QueryRowContext(context.Background(), `
		SELECT id, email, password_hash, created_at
		FROM users
		WHERE id = $1
	`, id).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt)
	if err == sql.ErrNoRows {
		return User{}, sql.ErrNoRows
	}
	return user, err
}

func authenticateUser(email, password string) (User, error) {
	user, err := getUserByEmail(email)
	if err != nil {
		return User{}, err
	}
	if !checkPasswordHash(password, user.PasswordHash) {
		return User{}, sql.ErrNoRows
	}
	return user, nil
}

func setAuthCookie(w http.ResponseWriter, user User) {
	http.SetCookie(w, &http.Cookie{
		Name:     "mba_user_id",
		Value:    strconv.Itoa(user.ID),
		Path:     "/",
		HttpOnly: true,
		MaxAge:   60 * 60 * 24 * 7,
	})
}

func clearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "mba_user_id",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func currentUser(r *http.Request) (User, bool, error) {
	cookie, err := r.Cookie("mba_user_id")
	if err != nil {
		if err == http.ErrNoCookie {
			return User{}, false, nil
		}
		return User{}, false, err
	}

	userID, err := strconv.Atoi(cookie.Value)
	if err != nil {
		return User{}, false, nil
	}

	user, err := getUserByID(userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return User{}, false, nil
		}
		return User{}, false, err
	}

	return user, true, nil
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		renderAuthPage(w, "login", "")
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "지원되지 않는 메서드입니다.", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := r.ParseForm(); err != nil {
		renderAuthPage(w, "login", "폼을 읽는 중 오류가 발생했습니다.")
		return
	}
	req.Email = r.FormValue("email")
	req.Password = r.FormValue("password")

	user, err := authenticateUser(req.Email, req.Password)
	if err != nil {
		renderAuthPage(w, "login", "이메일 또는 비밀번호가 올바르지 않습니다.")
		return
	}

	setAuthCookie(w, user)
	http.Redirect(w, r, "/", http.StatusFound)
}

func handleSignup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		renderAuthPage(w, "signup", "")
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "지원되지 않는 메서드입니다.", http.StatusMethodNotAllowed)
		return
	}

	var req SignupRequest
	if err := r.ParseForm(); err != nil {
		renderAuthPage(w, "signup", "폼을 읽는 중 오류가 발생했습니다.")
		return
	}
	req.Email = r.FormValue("email")
	req.Password = r.FormValue("password")

	if strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Password) == "" {
		renderAuthPage(w, "signup", "이메일과 비밀번호를 모두 입력해 주세요.")
		return
	}

	if _, err := getUserByEmail(req.Email); err == nil {
		renderAuthPage(w, "signup", "이미 사용 중인 이메일입니다.")
		return
	} else if err != sql.ErrNoRows {
		renderAuthPage(w, "signup", "회원가입 중 오류가 발생했습니다.")
		return
	}

	user, err := createUser(req.Email, req.Password)
	if err != nil {
		renderAuthPage(w, "signup", "회원가입 중 오류가 발생했습니다.")
		return
	}

	setAuthCookie(w, user)
	http.Redirect(w, r, "/", http.StatusFound)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	clearAuthCookie(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

func renderAuthPage(w http.ResponseWriter, mode string, errMessage string) {
	data := AuthPageData{Mode: mode, Error: errMessage}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := authTemplate.Execute(w, data); err != nil {
		http.Error(w, "인증 템플릿 렌더링 실패: "+err.Error(), http.StatusInternalServerError)
	}
}

func handleQuiz(w http.ResponseWriter, r *http.Request) {
	if len(globalProblems) == 0 {
		http.Error(w, "문제가 없습니다.", http.StatusInternalServerError)
		return
	}

	user, authenticated, err := currentUser(r)
	if err != nil {
		http.Error(w, "인증 정보 조회 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}
	if !authenticated {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	selectedCategory := r.URL.Query().Get("category")
	showBookmarksOnly := r.URL.Query().Get("bookmarks") == "1"
	problem := chooseProblem(r.URL.Query().Get("id"), selectedCategory, showBookmarksOnly, user.ID)
	isBookmarked, err := getBookmark(problem.ID, user.ID)
	if err != nil {
		http.Error(w, "즐겨찾기 상태 조회 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}

	renderData := RenderData{
		Problem:           problem,
		TableContent:      template.HTML(problem.TableContent),
		IsBookmarked:      isBookmarked,
		CategoryGroups:    categoryGroups,
		SelectedCategory:  selectedCategory,
		UserID:            user.ID,
		UserEmail:         user.Email,
		AccountInitial:    getAccountInitial(user.Email),
		IsAuthenticated:   true,
		ShowBookmarksOnly: showBookmarksOnly,
	}

	var buf bytes.Buffer
	if err := quizTemplate.Execute(&buf, renderData); err != nil {
		http.Error(w, "템플릿 렌더링 실패: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
}

func chooseProblem(idText string, category string, showBookmarksOnly bool, userID int) Problem {
	if idText != "" {
		id, err := strconv.Atoi(idText)
		if err == nil {
			if problem, ok := problemMap[id]; ok {
				return problem
			}
		}
	}

	filtered := globalProblems
	if category != "" {
		filtered = make([]Problem, 0, len(globalProblems))
		for _, p := range globalProblems {
			if p.Category == category {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			filtered = globalProblems
		}
	}

	if showBookmarksOnly {
		bookmarkIDs, err := getBookmarkedProblemIDs(userID)
		if err == nil && len(bookmarkIDs) > 0 {
			filtered = filterProblemsByIDs(filtered, bookmarkIDs)
			if len(filtered) == 0 {
				filtered = globalProblems
			}
		}
	}

	return filtered[mrand.Intn(len(filtered))]
}

func getBookmarkedProblemIDs(userID int) ([]int, error) {
	rows, err := db.QueryContext(context.Background(), `
		SELECT problem_id
		FROM user_progress
		WHERE user_id = $1 AND is_bookmarked = true
		ORDER BY problem_id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int, 0)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func getBookmark(problemID int, userID int) (bool, error) {
	var bookmarked bool
	err := db.QueryRowContext(context.Background(), "SELECT is_bookmarked FROM user_progress WHERE user_id = $1 AND problem_id = $2", userID, problemID).Scan(&bookmarked)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return bookmarked, err
}

func getProgress(problemID int, userID int) (Progress, error) {
	var p Progress
	err := db.QueryRowContext(context.Background(), "SELECT user_id, problem_id, interval_days, ease_factor, repetitions, next_review, is_bookmarked, wrong_count FROM user_progress WHERE user_id = $1 AND problem_id = $2", userID, problemID).
		Scan(&p.UserID, &p.ProblemID, &p.IntervalDays, &p.EaseFactor, &p.Repetitions, &p.NextReview, &p.IsBookmarked, &p.WrongCount)
	if err == sql.ErrNoRows {
		return Progress{UserID: userID, ProblemID: problemID, EaseFactor: 2.5, IntervalDays: 0, Repetitions: 0, IsBookmarked: false, WrongCount: 0}, nil
	}
	return p, err
}

func getProgressEntries(userID int) ([]Progress, error) {
	rows, err := db.QueryContext(context.Background(), `
		SELECT user_id, problem_id, interval_days, ease_factor, repetitions, next_review, is_bookmarked, wrong_count
		FROM user_progress
		WHERE user_id = $1
		ORDER BY problem_id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]Progress, 0)
	for rows.Next() {
		var p Progress
		if err := rows.Scan(&p.UserID, &p.ProblemID, &p.IntervalDays, &p.EaseFactor, &p.Repetitions, &p.NextReview, &p.IsBookmarked, &p.WrongCount); err != nil {
			return nil, err
		}
		entries = append(entries, p)
	}
	return entries, rows.Err()
}

func applyChartWidths(groups []WeaknessGroup) {
	maxWrong := 0
	for _, group := range groups {
		if group.WrongCount > maxWrong {
			maxWrong = group.WrongCount
		}
	}
	if maxWrong <= 0 {
		for i := range groups {
			groups[i].ChartWidth = 0
		}
		return
	}
	for i := range groups {
		if groups[i].WrongCount <= 0 {
			groups[i].ChartWidth = 0
			continue
		}
		groups[i].ChartWidth = (groups[i].WrongCount * 100) / maxWrong
		if groups[i].ChartWidth < 8 && groups[i].WrongCount > 0 {
			groups[i].ChartWidth = 8
		}
	}
}

func buildWeaknessGroups(problems []Problem, progressEntries []Progress) []WeaknessGroup {
	problemCategoryByID := make(map[int]string, len(problems))
	problemCountsByCategory := make(map[string]int)
	wrongCountsByCategory := make(map[string]int)

	for _, p := range problems {
		category := strings.TrimSpace(p.Category)
		if category == "" {
			continue
		}
		problemCategoryByID[p.ID] = category
		problemCountsByCategory[category]++
	}

	for _, entry := range progressEntries {
		if category, ok := problemCategoryByID[entry.ProblemID]; ok {
			wrongCountsByCategory[category] += entry.WrongCount
		}
	}

	groups := make([]WeaknessGroup, 0, len(categoryGroups))
	for _, group := range categoryGroups {
		groupStats := make([]CategoryStat, 0, len(group.Subcategories))
		for _, subcategory := range group.Subcategories {
			count := problemCountsByCategory[subcategory]
			if count == 0 {
				continue
			}
			groupStats = append(groupStats, CategoryStat{
				Name:         subcategory,
				WrongCount:   wrongCountsByCategory[subcategory],
				ProblemCount: count,
			})
		}

		groupWrongCount := 0
		for _, stat := range groupStats {
			groupWrongCount += stat.WrongCount
		}

		groups = append(groups, WeaknessGroup{
			Name:          group.Name,
			WrongCount:    groupWrongCount,
			Subcategories: groupStats,
		})
	}

	applyChartWidths(groups)
	return groups
}

func saveProgress(p Progress) error {
	var exists bool
	err := db.QueryRowContext(context.Background(), "SELECT EXISTS(SELECT 1 FROM user_progress WHERE user_id = $1 AND problem_id = $2)", p.UserID, p.ProblemID).Scan(&exists)
	if err != nil {
		return err
	}

	if exists {
		_, err = db.ExecContext(context.Background(), `
			UPDATE user_progress
			SET interval_days = $1,
				ease_factor = $2,
				repetitions = $3,
				next_review = $4,
				is_bookmarked = $5,
				wrong_count = $6,
				updated_at = NOW()
			WHERE user_id = $7 AND problem_id = $8
		`, p.IntervalDays, p.EaseFactor, p.Repetitions, p.NextReview, p.IsBookmarked, p.WrongCount, p.UserID, p.ProblemID)
	} else {
		_, err = db.ExecContext(context.Background(), `
			INSERT INTO user_progress (user_id, problem_id, interval_days, ease_factor, repetitions, next_review, is_bookmarked, wrong_count)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, p.UserID, p.ProblemID, p.IntervalDays, p.EaseFactor, p.Repetitions, p.NextReview, p.IsBookmarked, p.WrongCount)
	}
	return err
}

func saveBookmark(problemID int, userID int, bookmark bool) error {
	var exists bool
	err := db.QueryRowContext(context.Background(), "SELECT EXISTS(SELECT 1 FROM user_progress WHERE user_id = $1 AND problem_id = $2)", userID, problemID).Scan(&exists)
	if err != nil {
		return err
	}

	if exists {
		_, err = db.ExecContext(context.Background(), `
			UPDATE user_progress
			SET is_bookmarked = $1, updated_at = NOW()
			WHERE user_id = $2 AND problem_id = $3
		`, bookmark, userID, problemID)
	} else {
		_, err = db.ExecContext(context.Background(), `
			INSERT INTO user_progress (user_id, problem_id, interval_days, ease_factor, repetitions, next_review, is_bookmarked)
			VALUES ($1, $2, 0, 2.5, 0, NOW(), $3)
		`, userID, problemID, bookmark)
	}
	return err
}

func handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "지원되지 않는 메서드입니다.", http.StatusMethodNotAllowed)
		return
	}

	user, authenticated, err := currentUser(r)
	if err != nil {
		http.Error(w, "인증 정보 조회 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}
	if !authenticated {
		http.Error(w, "로그인이 필요합니다.", http.StatusUnauthorized)
		return
	}

	var req SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "요청 형식이 잘못되었습니다.", http.StatusBadRequest)
		return
	}

	problem, ok := problemMap[req.ProblemID]
	if !ok {
		http.Error(w, "문제를 찾을 수 없습니다.", http.StatusNotFound)
		return
	}

	isCorrect := req.SelectedOption == problem.Answer

	p, err := getProgress(req.ProblemID, user.ID)
	if err != nil {
		http.Error(w, "진도 정보 조회 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}

	if isCorrect {
		if p.Repetitions == 0 {
			p.IntervalDays = 1
		} else if p.Repetitions == 1 {
			p.IntervalDays = 3
		} else {
			p.IntervalDays = int(float64(p.IntervalDays) * p.EaseFactor)
		}
		p.Repetitions++
	} else {
		p.IntervalDays = 1
		p.Repetitions = 0
		p.WrongCount++
		p.EaseFactor -= 0.15
		if p.EaseFactor < 1.3 {
			p.EaseFactor = 1.3
		}
	}
	p.NextReview = time.Now().AddDate(0, 0, p.IntervalDays)

	if err := saveProgress(p); err != nil {
		http.Error(w, "진도 저장 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}

	headerJSON(w, http.StatusOK, map[string]interface{}{
		"is_correct":  isCorrect,
		"answer":      problem.Answer,
		"explanation": problem.Explanation,
	})
}

func handleWeaknesses(w http.ResponseWriter, r *http.Request) {
	user, authenticated, err := currentUser(r)
	if err != nil {
		http.Error(w, "인증 정보 조회 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}
	if !authenticated {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	entries, err := getProgressEntries(user.ID)
	if err != nil {
		http.Error(w, "진도 정보 조회 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}

	data := WeaknessesPageData{Groups: buildWeaknessGroups(globalProblems, entries)}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := weaknessesTemplate.Execute(w, data); err != nil {
		http.Error(w, "취약점 페이지 렌더링 실패: "+err.Error(), http.StatusInternalServerError)
	}
}

func handleFrequentWrong(w http.ResponseWriter, r *http.Request) {
	user, authenticated, err := currentUser(r)
	if err != nil {
		http.Error(w, "인증 정보 조회 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}
	if !authenticated {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	entries, err := getProgressEntries(user.ID)
	if err != nil {
		http.Error(w, "진도 정보 조회 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}

	problemByID := make(map[int]Problem, len(globalProblems))
	for _, p := range globalProblems {
		problemByID[p.ID] = p
	}

	problems := make([]FrequentWrongProblem, 0)
	for _, entry := range entries {
		if entry.WrongCount <= 0 {
			continue
		}
		problem, ok := problemByID[entry.ProblemID]
		if !ok {
			continue
		}
		problems = append(problems, FrequentWrongProblem{
			ID:           problem.ID,
			Category:     problem.Category,
			Question:     problem.Question,
			WrongCount:   entry.WrongCount,
			ProblemCount: 1,
		})
	}

	sort.Slice(problems, func(i, j int) bool {
		if problems[i].WrongCount == problems[j].WrongCount {
			return problems[i].ID < problems[j].ID
		}
		return problems[i].WrongCount > problems[j].WrongCount
	})

	data := FrequentWrongPageData{Problems: problems}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := frequentWrongTemplate.Execute(w, data); err != nil {
		http.Error(w, "자주 틀린 문제 페이지 렌더링 실패: "+err.Error(), http.StatusInternalServerError)
	}
}

func handleBookmark(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "지원되지 않는 메서드입니다.", http.StatusMethodNotAllowed)
		return
	}

	user, authenticated, err := currentUser(r)
	if err != nil {
		http.Error(w, "인증 정보 조회 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}
	if !authenticated {
		http.Error(w, "로그인이 필요합니다.", http.StatusUnauthorized)
		return
	}

	var req BookmarkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "요청 형식이 잘못되었습니다.", http.StatusBadRequest)
		return
	}

	if _, ok := problemMap[req.ProblemID]; !ok {
		http.Error(w, "문제를 찾을 수 없습니다.", http.StatusNotFound)
		return
	}

	if err := saveBookmark(req.ProblemID, user.ID, req.Bookmark); err != nil {
		http.Error(w, "즐겨찾기 저장 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func headerJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
