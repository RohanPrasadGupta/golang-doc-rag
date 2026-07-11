package models

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
