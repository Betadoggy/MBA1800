package main

import (
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
	ID             int               `json:"id"`
	OriginalNumber string            `json:"original_number"`
	Category       string            `json:"category"`
	Question       string            `json:"question"`
	BoxContent     string            `json:"box_content"`
	TableContent   string            `json:"table_content"`
	ImageURL       string            `json:"image_url"`
	HasImage       bool              `json:"has_image"`
	Options        map[string]string `json:"options"`
	Answer         int               `json:"answer"`
	Explanation    string            `json:"explanation"`
}

type RenderData struct {
	Problem
	TableContent template.HTML
	IsBookmarked bool
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

	problem := chooseProblem(r.URL.Query().Get("id"))
	isBookmarked, err := getBookmark(problem.ID)
	if err != nil {
		http.Error(w, "즐겨찾기 상태 조회 중 오류가 발생했습니다.", http.StatusInternalServerError)
		return
	}

	renderData := RenderData{
		Problem:      problem,
		TableContent: template.HTML(problem.TableContent),
		IsBookmarked: isBookmarked,
	}

	if err := quizTemplate.Execute(w, renderData); err != nil {
		http.Error(w, "템플릿 렌더링 실패: "+err.Error(), http.StatusInternalServerError)
	}
}

func chooseProblem(idText string) Problem {
	if idText != "" {
		id, err := strconv.Atoi(idText)
		if err == nil {
			if problem, ok := problemMap[id]; ok {
				return problem
			}
		}
	}

	return globalProblems[rand.Intn(len(globalProblems))]
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
