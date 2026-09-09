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
	secret string
	polkaKey string
}

type Token struct {
	Token string `json:"token"`
}

type User struct {
	ID uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email string `json:"email"`
	Token string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	IsChirpyRed bool `json:"is_chirpy_red"`
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
	sec := os.Getenv("SECRET")
	polkaKey := os.Getenv("POLKA_KEY")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("Error connecting to database %s", err)
		return
	}
	mux := http.NewServeMux()
	apiCfg := apiConfig {
		dbQueries: database.New(db),
		secret: sec,
		polkaKey: polkaKey,
	}
	apiCfg.fileServerHits.Store(0)
	mux.Handle("/app/", apiCfg.middlewareMetrics(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))

	mux.HandleFunc("POST /api/polka/webhooks", func (w http.ResponseWriter, r *http.Request) {
		// Validate api key
		apiKey, err := auth.GetAPIKey(r.Header)
		if err != nil {
			log.Printf("Error getting api key from request %s", err)
			w.WriteHeader(401)
			return
		}
		if apiKey != apiCfg.polkaKey {
			log.Printf("Wrong API Key")
			w.WriteHeader(401)
			return
		}

		// Get request body
		type data struct {
			UserId uuid.UUID `json:"user_id"`
		}
		type parameters struct {
			Event string `json:"event"`
			Data data `json:"data"`
		}
		
		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		err = decoder.Decode(&params)
		if err != nil {
			log.Printf("Error decoding parameters %s", err)
			w.WriteHeader(500)
			return
		}

		// Choose event
		if params.Event != "user.upgraded" {
			w.WriteHeader(204)
			return
		}

		err = apiCfg.dbQueries.UpgradeUser(r.Context(), params.Data.UserId)
		if err != nil {
			log.Printf("Error: %s", err)
			w.WriteHeader(404)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(204)
	})

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
			IsChirpyRed: dbUser.IsChirpyRed,
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

	mux.HandleFunc("PUT /api/users", func (w http.ResponseWriter, r *http.Request) {
		type parameters struct {
			Email string `json:"email"`
			Password string `json:"password"`
		}

		// Validate token
		token, err := auth.GetBearerToken(r.Header)
		if err != nil {
			log.Printf("No token provided %s", err)
			w.WriteHeader(401)
			return
		}

		userId, err := auth.ValidateJWT(token, apiCfg.secret)
		if err != nil {
			log.Printf("Invalid token %s", err)
			w.WriteHeader(401)
			return
		}
		
		// Hash password
		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		err = decoder.Decode(&params)
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


		// Updates User
		dbUser, err := apiCfg.dbQueries.UpdateUser(r.Context(), database.UpdateUserParams{
			ID: userId,
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
			IsChirpyRed: dbUser.IsChirpyRed,
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

		token, err := auth.MakeJWT(dbUser.ID, apiCfg.secret)

		if err != nil {
			log.Printf("Error decoding parameters %s", err)
			w.WriteHeader(500)
			return
		}

		refreshToken := auth.MakeRefreshToken()
		dbRefreshToken, err := apiCfg.dbQueries.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams{
			Token: refreshToken, 
			UserID: dbUser.ID,
			ExpiresAt: time.Now().AddDate(0, 0, 60),
		})
		respBody := User{
			ID: dbUser.ID,
			CreatedAt: dbUser.CreatedAt,
			UpdatedAt: dbUser.UpdatedAt,
			Email: dbUser.Email,
			Token: token,
			RefreshToken: dbRefreshToken.Token,
			IsChirpyRed: dbUser.IsChirpyRed,
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

	mux.HandleFunc("POST /api/refresh", func (w http.ResponseWriter, r *http.Request) {
		refreshToken := r.Header.Get("Authorization")
		refreshToken = strings.TrimPrefix(refreshToken, "Bearer ")

		dbRefreshToken, err := apiCfg.dbQueries.GetRefreshToken(r.Context(), refreshToken)
		if err != nil {
			log.Printf("No token found %s", err)
			w.WriteHeader(401)
			return
		}

		if dbRefreshToken.RevokedAt.Valid || time.Now().After(dbRefreshToken.ExpiresAt) {
			log.Printf("Token Expired")
			w.WriteHeader(401)
			return
		}

		token, err := auth.MakeJWT(dbRefreshToken.UserID, apiCfg.secret)
		if err != nil {
			log.Printf("Error Creating Token")
			w.WriteHeader(500)
			return
		}

		respBody := Token{
			Token: token,
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

	mux.HandleFunc("POST /api/revoke", func (w http.ResponseWriter, r *http.Request) {
		refreshToken := r.Header.Get("Authorization")
		refreshToken = strings.TrimPrefix(refreshToken, "Bearer ")

		log.Printf(refreshToken)

		err := apiCfg.dbQueries.RevokeRefreshToken(r.Context(), refreshToken)
		if err != nil {
			log.Printf("No token found")
			w.WriteHeader(401)
			return
		}

		w.WriteHeader(204)
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
		authorId := r.URL.Query().Get("author_id")
		// var dbChirps []database.Chirp
		// var err error
		// if authorId == "" {
		// 	dbChirps, err = apiCfg.dbQueries.GetChirps(r.Context())
		// 	if err != nil {
		// 		log.Printf("Error connecting to database %s", err)
		// 		w.WriteHeader(500)
		// 		return
		// 	}
		// } else {
		authorUUID, err := uuid.Parse(authorId)
		// 	if err != nil {
		// 		log.Printf("Unable to parse author id into uuid ", err)
		// 		w.WriteHeader(400)
		// 		return
		// 	}
		// }
		dbChirps, err := apiCfg.dbQueries.GetChirpsByAuthor(r.Context(), authorUUID)
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
		}
		
		decoder := json.NewDecoder(r.Body)
		params := parameters{}
		err := decoder.Decode(&params)
		if err != nil {
			log.Printf("Error decoding parameters %s", err)
			w.WriteHeader(500)
			return
		}

		// Validates Token
		token, err := auth.GetBearerToken(r.Header)
		if err != nil {
			log.Printf("No token provided %s", err)
			w.WriteHeader(401)
			return
		}

		userId, err := auth.ValidateJWT(token, apiCfg.secret)
		if err != nil {
			log.Printf("Invalid token %s", err)
			w.WriteHeader(401)
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
				UserID: userId,
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

	mux.HandleFunc("DELETE /api/chirps/{chirpID}", func (w http.ResponseWriter, r *http.Request) {
		// Validates Token
		token, err := auth.GetBearerToken(r.Header)
		if err != nil {
			log.Printf("No token provided %s", err)
			w.WriteHeader(401)
			return
		}

		userId, err := auth.ValidateJWT(token, apiCfg.secret)
		if err != nil {
			log.Printf("Invalid token %s", err)
			w.WriteHeader(403)
			return
		}

		// Attempts to get the provided id
		stringId := r.PathValue("chirpID")
		chirpId , err := uuid.Parse(stringId)
		if err != nil {
			log.Printf("Error parsing chirp id%s", err)
			w.WriteHeader(300)
			return
		}
		chirp, err := apiCfg.dbQueries.GetChirp(r.Context(), chirpId)
		if err != nil {
			w.WriteHeader(404)
			return
		}

		// Validates the user owns the chirp
		if userId != chirp.UserID {
			log.Printf("Unable to delete some else's chirp")
			w.WriteHeader(403)
			return
		}

		// Delete Chirps
		err = apiCfg.dbQueries.DeleteChirp(r.Context(), chirpId)
		if err != nil {
			log.Printf("Error deleting chirp id%s", err)
			w.WriteHeader(300)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(204)
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

