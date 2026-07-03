package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"os"
	"strconv"
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

type RenderData struct {
	Problem
	TableContent     template.HTML
	IsBookmarked     bool
	CategoryGroups   []CategoryGroup
	SelectedCategory string
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

type Progress struct {
	ProblemID    int       `json:"problem_id"`
	IntervalDays int       `json:"interval_days"`
	EaseFactor   float64   `json:"ease_factor"`
	Repetitions  int       `json:"repetitions"`
	NextReview   time.Time `json:"next_review"`
	IsBookmarked bool      `json:"is_bookmarked"`
}

var (
	globalProblems []Problem
	problemMap     map[int]Problem
	quizTemplate   *template.Template
	db             *sql.DB
	categoryGroups = []CategoryGroup{
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
	rand.Seed(time.Now().UnixNano())

	if err := loadProblems("data.json"); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	problemMap = buildProblemMap(globalProblems)
	fmt.Printf("성공적으로 %d개의 문제를 로드했습니다.\n", len(globalProblems))

	quizTemplate = template.Must(template.ParseFiles("templates/quiz.html"))

	var err error
	db, err = openDB(getDatabaseURL())
	if err != nil {
		panic(err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleQuiz)
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

func handleQuiz(w http.ResponseWriter, r *http.Request) {
	if len(globalProblems) == 0 {
		http.Error(w, "문제가 없습니다.", http.StatusInternalServerError)
		return
	}

	selectedCategory := r.URL.Query().Get("category")
	problem := chooseProblem(r.URL.Query().Get("id"), selectedCategory)
	isBookmarked, err := getBookmark(problem.ID)
	if err != nil {
		http.Error(w, "즐겨찾기 상태 조회 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}

	renderData := RenderData{
		Problem:          problem,
		TableContent:     template.HTML(problem.TableContent),
		IsBookmarked:     isBookmarked,
		CategoryGroups:   categoryGroups,
		SelectedCategory: selectedCategory,
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

func chooseProblem(idText string, category string) Problem {
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

	return filtered[rand.Intn(len(filtered))]
}

func getBookmark(problemID int) (bool, error) {
	var bookmarked bool
	err := db.QueryRowContext(context.Background(), "SELECT is_bookmarked FROM user_progress WHERE problem_id = $1", problemID).Scan(&bookmarked)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return bookmarked, err
}

func getProgress(problemID int) (Progress, error) {
	var p Progress
	err := db.QueryRowContext(context.Background(), "SELECT problem_id, interval_days, ease_factor, repetitions, next_review, is_bookmarked FROM user_progress WHERE problem_id = $1", problemID).
		Scan(&p.ProblemID, &p.IntervalDays, &p.EaseFactor, &p.Repetitions, &p.NextReview, &p.IsBookmarked)
	if err == sql.ErrNoRows {
		return Progress{ProblemID: problemID, EaseFactor: 2.5, IntervalDays: 0, Repetitions: 0, IsBookmarked: false}, nil
	}
	return p, err
}

func saveProgress(p Progress) error {
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO user_progress (problem_id, interval_days, ease_factor, repetitions, next_review, is_bookmarked)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (problem_id)
		DO UPDATE SET interval_days = $2, ease_factor = $3, repetitions = $4, next_review = $5, is_bookmarked = $6;
	`, p.ProblemID, p.IntervalDays, p.EaseFactor, p.Repetitions, p.NextReview, p.IsBookmarked)
	return err
}

func saveBookmark(problemID int, bookmark bool) error {
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO user_progress (problem_id, interval_days, ease_factor, repetitions, next_review, is_bookmarked)
		VALUES ($1, 0, 2.5, 0, NOW(), $2)
		ON CONFLICT (problem_id)
		DO UPDATE SET is_bookmarked = $2;
	`, problemID, bookmark)
	return err
}

func handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "지원되지 않는 메서드입니다.", http.StatusMethodNotAllowed)
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

	p, err := getProgress(req.ProblemID)
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

func handleBookmark(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "지원되지 않는 메서드입니다.", http.StatusMethodNotAllowed)
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

	if err := saveBookmark(req.ProblemID, req.Bookmark); err != nil {
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
