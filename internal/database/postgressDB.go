package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

type Document struct {
	ID         string    `json:"id"`
	Filename   string    `json:"filename"`
	S3Path     string    `json:"s3_path"`
	ChunkCount int       `json:"chunk_count"`
	CreatedAt  time.Time `json:"created_at"`
}

type ResumeAnalysis struct {
	ID                        string   `json:"id,omitempty"`
	Filename                  string   `json:"filename,omitempty"`
	S3Path                    string   `json:"s3_path,omitempty"`
	ChunkCount                int      `json:"chunk_count,omitempty"`
	Skills                    []string `json:"skills"`
	ExperienceKeywords        []string `json:"experience_keywords"`
	JobTitles                 []string `json:"job_titles"`
	ProjectKeywords           []string `json:"project_keywords"`
	EducationKeywords         []string `json:"education_keywords"`
	Certifications            []string `json:"certifications"`
	DomainKeywords            []string `json:"domain_keywords"`
	SoftSkills                []string `json:"soft_skills"`
	ActionVerbs               []string `json:"action_verbs"`
	QuantifiedAchievements    []string `json:"quantified_achievements"`
	ExplicitYearsOfExperience []string `json:"explicit_years_of_experience"`
}

func NewPostgres(ctx context.Context) (*PostgresStore, error) {
	databaseURL := os.Getenv("POSTGRES_DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("POSTGRES_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	err = pool.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("ping pool: %w", err)
	}
	log.Println("connected to Postgres")
	return &PostgresStore{pool: pool}, nil
}

func (p *PostgresStore) SaveDocument(ctx context.Context, id, filename, s3Path string, chunkCount int) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO documents (id, filename, s3_path, chunk_count) VALUES ($1, $2, $3, $4)`,
		id, filename, s3Path, chunkCount,
	)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

func (p *PostgresStore) ListDocuments(ctx context.Context) ([]Document, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, filename, s3_path, chunk_count, created_at
		 FROM documents ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query documents: %w", err)
	}
	defer rows.Close()

	var documents []Document
	for rows.Next() {
		var doc Document
		err := rows.Scan(&doc.ID, &doc.Filename, &doc.S3Path, &doc.ChunkCount, &doc.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		documents = append(documents, doc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents: %w", err)
	}

	return documents, nil
}

func (p *PostgresStore) DeleteDocument(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx,
		`DELETE FROM documents WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	return nil
}

// resume analysis postgress functions

func (p *PostgresStore) SaveResumeAnalysis(
	ctx context.Context,
	id string,
	filename string,
	s3Path string,
	chunkCount int,
	extractedSkills ResumeAnalysis,
) error {

	_, err := p.pool.Exec(
		ctx,
		`
		INSERT INTO resume_analysis (
			id,
			filename,
			s3_path,
			chunk_count,
			skills,
			experience_keywords,
			job_titles,
			project_keywords,
			education_keywords,
			certifications,
			domain_keywords,
			soft_skills,
			action_verbs,
			quantified_achievements,
			explicit_years_of_experience
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$11,
			$12,
			$13,
			$14,
			$15
		)
		`,
		id,                                        // $1
		filename,                                  // $2
		s3Path,                                    // $3
		chunkCount,                                // $4
		extractedSkills.Skills,                    // $5
		extractedSkills.ExperienceKeywords,        // $6
		extractedSkills.JobTitles,                 // $7
		extractedSkills.ProjectKeywords,           // $8
		extractedSkills.EducationKeywords,         // $9
		extractedSkills.Certifications,            // $10
		extractedSkills.DomainKeywords,            // $11
		extractedSkills.SoftSkills,                // $12
		extractedSkills.ActionVerbs,               // $13
		extractedSkills.QuantifiedAchievements,    // $14
		extractedSkills.ExplicitYearsOfExperience, // $15
	)

	if err != nil {
		return fmt.Errorf("insert resume analysis: %w", err)
	}
	return nil
}

func (p *PostgresStore) GetResumeAnalysis(ctx context.Context, id string) (ResumeAnalysis, error) {
	row := p.pool.QueryRow(ctx,
		`SELECT skills, experience_keywords, job_titles, project_keywords, education_keywords, certifications, domain_keywords, soft_skills, action_verbs, quantified_achievements, explicit_years_of_experience FROM resume_analysis WHERE id = $1`,
		id,
	)
	var analysis ResumeAnalysis
	err := row.Scan(&analysis.Skills, &analysis.ExperienceKeywords, &analysis.JobTitles, &analysis.ProjectKeywords, &analysis.EducationKeywords, &analysis.Certifications, &analysis.DomainKeywords, &analysis.SoftSkills, &analysis.ActionVerbs, &analysis.QuantifiedAchievements, &analysis.ExplicitYearsOfExperience)
	if err != nil {
		return ResumeAnalysis{}, fmt.Errorf("scan resume analysis: %w", err)
	}
	return analysis, nil
}

func (p *PostgresStore) GetAllResumeAnalysis(ctx context.Context) ([]ResumeAnalysis, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, filename, s3_path, chunk_count,
		        skills, experience_keywords, job_titles, project_keywords, education_keywords,
		        certifications, domain_keywords, soft_skills, action_verbs,
		        quantified_achievements, explicit_years_of_experience
		 FROM resume_analysis
		 ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query resume analysis: %w", err)
	}
	defer rows.Close()

	var analyses []ResumeAnalysis
	for rows.Next() {
		var analysis ResumeAnalysis
		err := rows.Scan(
			&analysis.ID,
			&analysis.Filename,
			&analysis.S3Path,
			&analysis.ChunkCount,
			&analysis.Skills,
			&analysis.ExperienceKeywords,
			&analysis.JobTitles,
			&analysis.ProjectKeywords,
			&analysis.EducationKeywords,
			&analysis.Certifications,
			&analysis.DomainKeywords,
			&analysis.SoftSkills,
			&analysis.ActionVerbs,
			&analysis.QuantifiedAchievements,
			&analysis.ExplicitYearsOfExperience,
		)
		if err != nil {
			return nil, fmt.Errorf("scan resume analysis: %w", err)
		}
		analyses = append(analyses, analysis)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resume analysis: %w", err)
	}
	return analyses, nil
}

func (p *PostgresStore) DeleteResumeAnalysis(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx,
		`DELETE FROM resume_analysis WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("delete resume analysis: %w", err)
	}
	return nil
}
