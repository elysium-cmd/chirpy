package main

import (
	"os"
	"log"
	"fmt"
	"strings"
	"time"
	"encoding/json"
	"sync/atomic"
	"net/http"
	"database/sql"
	"github.com/joho/godotenv"
	"github.com/elysium-cmd/chirpy/internal/database"
	"github.com/elysium-cmd/chirpy/internal/auth"
	"github.com/google/uuid"
)
import _ "github.com/lib/pq"

type apiConfig struct {
	fileServerHits atomic.Int32
	dbQueries *database.Queries
}

type User struct {
	ID uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email string `json:"email"`
}

type Chirp struct {
	ID uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body string `json:"body"`
	UserId uuid.UUID `json:"user_id"`
}

type errors struct {
	Error string `json:"error"`
}

func (cfg *apiConfig) middlewareMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileServerHits.Store(cfg.fileServerHits.Add(1))
		next.ServeHTTP(w, r)
	})
}

func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	w.Write([]byte("OK"))
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	env := os.Getenv("PLATFORM")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("Error connecting to database %s", err)
		return
	}
	mux := http.NewServeMux()
	apiCfg := apiConfig {
		dbQueries: database.New(db),
	}
	apiCfg.fileServerHits.Store(0)
	mux.Handle("/app/", apiCfg.middlewareMetrics(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.HandleFunc("POST /api/users", func (w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Email string `json:"email"`
			Password string `json:"password"`
		}
		
		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		err := decoder.Decode(&params)
		if err != nil {
			log.Printf("Error decoding parameters %s", err)
			w.WriteHeader(500)
			return
		}

		hash, err := auth.HashPassword(params.Password)
		if err != nil {
			log.Printf("Error hashing password %s", err)
			w.WriteHeader(500)
			return
		}

		dbUser, err := apiCfg.dbQueries.CreateUser(r.Context(), database.CreateUserParams{
			Email: params.Email, 
			HashedPassword: hash,
		})
		if err != nil {
			log.Printf("Error connecting to database %s", err)
			w.WriteHeader(500)
			return
		}
		respBody := User{
			ID: dbUser.ID,
			CreatedAt: dbUser.CreatedAt,
			UpdatedAt: dbUser.UpdatedAt,
			Email: dbUser.Email,
		}
		data, err := json.Marshal(respBody)
		if err != nil {
			log.Printf("Error marshalling JSON: %s", err)
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write(data)
	})
	mux.HandleFunc("POST /api/login", func (w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Email string `json:"email"`
			Password string `json:"password"`
		}
		
		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		err := decoder.Decode(&params)
		if err != nil {
			log.Printf("Error decoding parameters %s", err)
			w.WriteHeader(500)
			return
		}

		dbUser, err := apiCfg.dbQueries.GetUserByEmail(r.Context(), params.Email)
		if err != nil {
			w.WriteHeader(401)
			w.Write([]byte(fmt.Sprintf("incorrect email or password")))
			return
		}

		match, err := auth.CheckPasswordHash(params.Password, dbUser.HashedPassword)
		if err != nil {
			w.WriteHeader(401)
			w.Write([]byte(fmt.Sprintf("incorrect email or password")))
			return
		}
		
		if !match {
			w.WriteHeader(401)
			w.Write([]byte(fmt.Sprintf("incorrect email or password")))
			return
		}

		respBody := User{
			ID: dbUser.ID,
			CreatedAt: dbUser.CreatedAt,
			UpdatedAt: dbUser.UpdatedAt,
			Email: dbUser.Email,
		}
		data, err := json.Marshal(respBody)
		if err != nil {
			log.Printf("Error marshalling JSON: %s", err)
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(data)
	})
	mux.HandleFunc("GET /api/chirps/{chirpID}", func (w http.ResponseWriter, r *http.Request) {
		stringId := r.PathValue("chirpID")
		uuId, err := uuid.Parse(stringId)
		if err != nil {
			log.Printf("Error parsing chirp id%s", err)
			w.WriteHeader(300)
			return
		}

		dbChirp, err := apiCfg.dbQueries.GetChirp(r.Context(), uuId)
		if err != nil {
			if strings.Contains(fmt.Sprintf("%s", err), "no rows in result set") {
				log.Printf("No Chirp Found")
				w.WriteHeader(404)
				return
			}
			log.Printf("Error connecting to database %s", err)
			w.WriteHeader(500)
			return
		}

		respBody := Chirp{
			ID: dbChirp.ID,
			CreatedAt: dbChirp.CreatedAt,
			UpdatedAt: dbChirp.UpdatedAt,
			Body: dbChirp.Body,
			UserId: dbChirp.UserID,
		}
		data, err := json.Marshal(respBody)
		if err != nil {
			log.Printf("Error marshalling JSON: %s", err)
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(data)
	})
	mux.HandleFunc("GET /api/chirps", func (w http.ResponseWriter, r *http.Request) {
		dbChirps, err := apiCfg.dbQueries.GetChirps(r.Context())
		if err != nil {
			log.Printf("Error connecting to database %s", err)
			w.WriteHeader(500)
			return
		}

		var respBody []Chirp
		for _, dbChirp := range dbChirps {
			respRow := Chirp{
				ID: dbChirp.ID,
				CreatedAt: dbChirp.CreatedAt,
				UpdatedAt: dbChirp.UpdatedAt,
				Body: dbChirp.Body,
				UserId: dbChirp.UserID,
			}
			respBody = append(respBody, respRow)
		}
		data, err := json.Marshal(respBody)
		if err != nil {
			log.Printf("Error marshalling JSON: %s", err)
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(data)
	})
	mux.HandleFunc("POST /api/chirps", func (w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Body string `json:"body"`
			UserId uuid.UUID `json:"user_id"`
		}
		
		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		err := decoder.Decode(&params)
		if err != nil {
			log.Printf("Error decoding parameters %s", err)
			w.WriteHeader(500)
			return
		}

		// Validate Chirp
		if len(params.Body) > 140 {
			respBody := errors{
				Error: "Chirp is too long",
			}
			data, err := json.Marshal(respBody)
			if err != nil {
				log.Printf("Error marshalling JSON: %s", err)
				w.WriteHeader(500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			w.Write(data)
		} else {
			cleanedBody := ""
			splitBody := strings.Split(params.Body, " ")
			for index := range splitBody {
				lowerCaseWord := strings.ToLower(splitBody[index])
				if lowerCaseWord == "kerfuffle" || lowerCaseWord == "sharbert" || lowerCaseWord == "fornax"{
					splitBody[index] = "****"
				}
			}
			cleanedBody = strings.Join(splitBody, " ")

			dbChirp, err := apiCfg.dbQueries.CreateChirp(r.Context(), database.CreateChirpParams{
				Body: cleanedBody,
				UserID: params.UserId,
			})
			if err != nil {
				log.Printf("Error connecting to database %s", err)
				w.WriteHeader(500)
				return
			}
			respBody := Chirp{
				ID: dbChirp.ID,
				CreatedAt: dbChirp.CreatedAt,
				UpdatedAt: dbChirp.UpdatedAt,
				Body: dbChirp.Body,
				UserId: dbChirp.UserID,
			}
			data, err := json.Marshal(respBody)
			if err != nil {
				log.Printf("Error marshalling JSON: %s", err)
				w.WriteHeader(500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			w.Write(data)
		}
	})

	mux.HandleFunc("GET /api/healthz", health)
	mux.HandleFunc("GET /admin/metrics", func(w http.ResponseWriter, r *http.Request){
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf("<html><body><h1>Welcome, Chirpy Admin</h1><p>Chirpy has been visited %d times!</p></body></html>", apiCfg.fileServerHits.Load())))
	})
	mux.HandleFunc("POST /admin/reset", func(w http.ResponseWriter, r *http.Request){
		if env != "dev" {
			w.WriteHeader(403)
			return
		}
		err := apiCfg.dbQueries.DeleteUsers(r.Context())
		if err != nil {
			log.Printf("Error connecting to database %s", err)
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	})
	server := http.Server{
		Addr: ":8080",
		Handler: mux,
	}
	server.ListenAndServe()
}

