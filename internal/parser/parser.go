package parser

// Kind describes what a file does with a variable.
type Kind string

const (
	// KindDefinition supplies a value: DATABASE_URL=postgres://localhost.
	KindDefinition Kind = "definition"

	// KindReference reads a value from elsewhere: ${DATABASE_URL}.
	KindReference Kind = "reference"

	// KindConsumption reads a value at runtime: os.Getenv("DATABASE_URL").
	KindConsumption Kind = "consumption"
)

// Location is a slash-separated path relative to the scan root, plus a line.
type Location struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// Occurrence is a single mention of a variable in a single file.
type Occurrence struct {
	Name     string   `json:"name"`
	Kind     Kind     `json:"kind"`
	Value    string   `json:"value,omitempty"`
	Location Location `json:"location"`

	// Service is set when the occurrence belongs to a container definition.
	Service string `json:"service,omitempty"`

	// HasDefault marks ${VAR:-fallback}, which can never resolve to nothing and therefore counts as a source.
	HasDefault bool `json:"hasDefault,omitempty"`

	// DerivedFrom names the variables this value is built from, as in "DB_HOST: ${POSTGRES_HOST}". Supplied exactly when all of them are.
	DerivedFrom []string `json:"derivedFrom,omitempty"`

	// Origin names an external provider of the value, such as a GitHub secret. The repository cannot show the value, but it does know something supplies it.
	Origin string `json:"origin,omitempty"`
}

// Service is a container or job — a named runtime context an infrastructure file declares.
type Service struct {
	Name     string   `json:"name"`
	Location Location `json:"location"`

	// EnvFiles are relative to the scan root, not to the compose file.
	EnvFiles []string `json:"envFiles,omitempty"`
}

// Result is what a parser returns for one file.
type Result struct {
	Occurrences []Occurrence
	Services    []Service
}
