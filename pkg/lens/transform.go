package lens

import (
	"context"
	"fmt"
	"io/ioutil"
	"sync"
	"strings"
	"strconv"
	"os"
	"time"
	"regexp"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// TransformConfig represents configuration for data transformation
type TransformConfig struct {
	// Pipeline configurations
	Pipelines map[string]string `yaml:"pipelines"`
	// Default pipeline to use
	DefaultPipeline string `yaml:"defaultPipeline"`
	// Options for the transformer
	Options struct {
		MaxConcurrency int  `yaml:"maxConcurrency"`
		BufferSize     int  `yaml:"bufferSize"`
		EnableMetrics  bool `yaml:"enableMetrics"`
	} `yaml:"options"`
}

// Pipeline represents a data transformation pipeline
type Pipeline struct {
	Name        string                 `yaml:"name"`
	Steps       []TransformStep        `yaml:"steps"`
	Variables   map[string]interface{} `yaml:"variables"`
	loaded      bool
	transformer *Transformer
}

// TransformStep represents a single transformation step
type TransformStep struct {
	Name   string                 `yaml:"name"`
	Type   string                 `yaml:"type"`
	Config map[string]interface{} `yaml:"config"`
}

// Transformer handles data transformation using the LensVM pattern
type Transformer struct {
	pipelines map[string]*Pipeline
	logger    *zap.Logger
	config    *TransformConfig
	metrics   *TransformMetrics
	mu        sync.RWMutex
}

// TransformMetrics tracks transformation statistics
type TransformMetrics struct {
	mu               sync.RWMutex
	transformCount   int64
	errorCount       int64
	pipelineMetrics  map[string]int64
	lastError        error
	lastErrorTime    string
	processingTime   int64
	successfulSteps  map[string]int64
	failedSteps      map[string]int64
}

// NewTransformer creates a new Transformer instance
func NewTransformer(config *TransformConfig, logger *zap.Logger) (*Transformer, error) {
	t := &Transformer{
		pipelines: make(map[string]*Pipeline),
		logger:    logger,
		config:    config,
		metrics: &TransformMetrics{
			pipelineMetrics: make(map[string]int64),
			successfulSteps: make(map[string]int64),
			failedSteps:    make(map[string]int64),
		},
	}

	// Load all configured pipelines
	for name, path := range config.Pipelines {
		if err := t.LoadPipeline(name, path); err != nil {
			return nil, fmt.Errorf("failed to load pipeline %s: %w", name, err)
		}
	}

	return t, nil
}

// LoadPipeline loads a transformation pipeline from a file
func (t *Transformer) LoadPipeline(name, path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Read pipeline configuration
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read pipeline file: %w", err)
	}

	var pipeline Pipeline
	if err := yaml.Unmarshal(data, &pipeline); err != nil {
		return fmt.Errorf("failed to parse pipeline: %w", err)
	}

	pipeline.Name = name
	pipeline.loaded = true
	pipeline.transformer = t

	t.pipelines[name] = &pipeline
	return nil
}

// Transform applies a pipeline to input data
func (t *Transformer) Transform(ctx context.Context, pipelineName string, data map[string]interface{}) (map[string]interface{}, error) {
	t.mu.RLock()
	pipeline, ok := t.pipelines[pipelineName]
	t.mu.RUnlock()

	if !ok {
		if pipelineName == "" && t.config.DefaultPipeline != "" {
			return t.Transform(ctx, t.config.DefaultPipeline, data)
		}
		return nil, fmt.Errorf("pipeline %s not found", pipelineName)
	}

	t.logger.Debug("Starting transformation",
		zap.String("pipeline", pipelineName),
		zap.Any("input_data", data))

	result := make(map[string]interface{})
	for k, v := range data {
		result[k] = v
	}

	for _, step := range pipeline.Steps {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			t.logger.Debug("Executing step",
				zap.String("pipeline", pipelineName),
				zap.String("step", step.Name),
				zap.Any("data", result))

			var err error
			result, err = t.executeStep(step, result)
			if err != nil {
				t.logger.Error("Step execution failed",
					zap.String("pipeline", pipelineName),
					zap.String("step", step.Name),
					zap.Error(err))
				t.recordError(pipelineName, step.Name, err)
				return nil, fmt.Errorf("failed to execute step %s: %w", step.Name, err)
			}
			t.recordSuccess(pipelineName, step.Name)
			
			t.logger.Debug("Step completed",
				zap.String("pipeline", pipelineName),
				zap.String("step", step.Name),
				zap.Any("result", result))
		}
	}

	return result, nil
}

// executeStep executes a single transformation step
func (t *Transformer) executeStep(step TransformStep, data map[string]interface{}) (map[string]interface{}, error) {
	t.logger.Debug("Executing transformation step",
		zap.String("step_type", step.Type),
		zap.Any("config", step.Config),
		zap.Any("input_data", data))

	var result map[string]interface{}
	var err error

	switch step.Type {
	case "normalize":
		result, err = t.normalizeData(data, step.Config)
	case "validate":
		result, err = t.validateData(data, step.Config)
	case "enrich":
		result, err = t.enrichData(data, step.Config)
	case "filter":
		result, err = t.filterData(data, step.Config)
	default:
		err = fmt.Errorf("unknown step type: %s", step.Type)
	}

	if err != nil {
		t.logger.Error("Step execution failed",
			zap.String("step_type", step.Type),
			zap.Error(err))
		return nil, err
	}

	t.logger.Debug("Step execution completed",
		zap.String("step_type", step.Type),
		zap.Any("output_data", result))

	return result, nil
}

// normalizeData normalizes the input data according to the provided configuration
func (t *Transformer) normalizeData(data map[string]interface{}, config map[string]interface{}) (map[string]interface{}, error) {
	t.logger.Debug("Starting data normalization",
		zap.Any("config", config),
		zap.Any("input_data", data))

	result := make(map[string]interface{})
	for k, v := range data {
		result[k] = v
	}

	// Handle field normalization
	if fields, ok := config["fields"].([]interface{}); ok {
		for _, field := range fields {
			fieldName := field.(string)
			if value, exists := result[fieldName]; exists {
				t.logger.Debug("Normalizing field",
					zap.String("field", fieldName),
					zap.Any("value", value))

				switch v := value.(type) {
				case string:
					if strings.HasPrefix(v, "0x") {
						result[fieldName] = strings.ToLower(v)
					}
				case float64:
					if format, ok := config["format"].(string); ok && format == "hex" {
						result[fieldName] = fmt.Sprintf("0x%x", int64(v))
					}
				case []interface{}:
					// Handle array fields (e.g., topics)
					for i, item := range v {
						if str, ok := item.(string); ok && strings.HasPrefix(str, "0x") {
							v[i] = strings.ToLower(str)
						}
					}
					result[fieldName] = v
				default:
					t.logger.Debug("Unexpected field type",
						zap.String("field", fieldName),
						zap.String("type", fmt.Sprintf("%T", value)))
				}
			} else {
				t.logger.Debug("Field not found in data",
					zap.String("field", fieldName))
			}
		}
	}

	t.logger.Debug("Data normalization completed",
		zap.Any("output_data", result))

	return result, nil
}

// validateData validates the input data according to the provided configuration
func (t *Transformer) validateData(data map[string]interface{}, config map[string]interface{}) (map[string]interface{}, error) {
	if rules, ok := config["rules"].([]interface{}); ok {
		for _, rule := range rules {
			ruleMap := rule.(map[string]interface{})
			field := ruleMap["field"].(string)
			value, exists := data[field]

			// Check required fields
			if required, ok := ruleMap["required"].(bool); ok && required {
				if !exists || value == nil {
					return nil, fmt.Errorf("required field %s is missing", field)
				}
			}

			// Validate format if specified
			if format, ok := ruleMap["format"].(string); ok && exists {
				if str, ok := value.(string); ok {
					matched, err := regexp.MatchString(format, str)
					if err != nil {
						return nil, fmt.Errorf("invalid format pattern for field %s: %w", field, err)
					}
					if !matched {
						return nil, fmt.Errorf("field %s does not match required format", field)
					}
				}
			}

			// Validate type if specified
			if typ, ok := ruleMap["type"].(string); ok && exists {
				switch typ {
				case "number":
					if _, ok := value.(float64); !ok {
						return nil, fmt.Errorf("field %s must be a number", field)
					}
				case "array":
					if _, ok := value.([]interface{}); !ok {
						return nil, fmt.Errorf("field %s must be an array", field)
					}
				}
			}

			// Validate numeric constraints
			if min, ok := ruleMap["min"].(float64); ok {
				if num, ok := value.(float64); ok {
					if num < min {
						return nil, fmt.Errorf("field %s must be >= %v", field, min)
					}
				}
			}
		}
	}

	return data, nil
}

// enrichData enriches the input data according to the provided configuration
func (t *Transformer) enrichData(data map[string]interface{}, config map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	for k, v := range data {
		result[k] = v
	}

	// Add computed fields
	if fields, ok := config["fields"].(map[string]interface{}); ok {
		for field, expr := range fields {
			switch e := expr.(type) {
			case string:
				// Handle special fields
				if field == "version" {
					result[field] = "1.0" // Hardcoded version for now
					continue
				}

				// Handle simple expressions
				switch e {
				case "now()":
					result[field] = time.Now().UTC().Format(time.RFC3339)
				default:
					// Handle environment variables
					if strings.HasPrefix(e, "${") && strings.HasSuffix(e, "}") {
						envVar := strings.TrimSuffix(strings.TrimPrefix(e, "${"), "}")
						result[field] = os.Getenv(envVar)
						continue
					}

					// Try to evaluate the expression
					value, err := t.evaluateExpression(e, data)
					if err != nil {
						t.logger.Error("Failed to evaluate expression",
							zap.String("field", field),
							zap.String("expr", e),
							zap.Error(err))
						continue
					}
					result[field] = value
				}
			case map[string]interface{}:
				// Handle complex field computations
				computed := make(map[string]interface{})
				for k, v := range e {
					if str, ok := v.(string); ok {
						value, err := t.evaluateExpression(str, data)
						if err != nil {
							t.logger.Error("Failed to evaluate expression",
								zap.String("field", k),
								zap.String("expr", str),
								zap.Error(err))
							continue
						}
						computed[k] = value
					} else {
						computed[k] = v
					}
				}
				result[field] = computed
			}
		}
	}

	return result, nil
}

// filterData filters the input data according to the provided configuration
func (t *Transformer) filterData(data map[string]interface{}, config map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	// If fields are specified, only keep those fields
	if fields, ok := config["fields"].([]interface{}); ok {
		// Create a map for O(1) field lookup
		allowedFields := make(map[string]bool)
		for _, field := range fields {
			if fieldName, ok := field.(string); ok {
				allowedFields[fieldName] = true
			}
		}

		// Only copy allowed fields
		for k, v := range data {
			if allowedFields[k] {
				result[k] = v
			}
		}
		return result, nil
	}

	// If no fields specified, copy all fields
	for k, v := range data {
		result[k] = v
	}

	// Apply filters based on conditions
	if conditions, ok := config["conditions"].([]interface{}); ok {
		for _, cond := range conditions {
			condition := cond.(map[string]interface{})
			if ifExpr, ok := condition["if"].(string); ok {
				matches, err := t.evaluateCondition(ifExpr, data)
				if err != nil {
					return nil, fmt.Errorf("failed to evaluate condition: %w", err)
				}
				if matches {
					if then, ok := condition["then"].(map[string]interface{}); ok {
						for k, v := range then {
							result[k] = v
						}
					}
				}
			}
		}
	}

	return result, nil
}

// evaluateExpression evaluates a simple expression against the data
func (t *Transformer) evaluateExpression(expr string, data map[string]interface{}) (interface{}, error) {
	// Handle empty expression
	if expr == "" {
		return "", nil
	}

	// Handle environment variables
	if strings.HasPrefix(expr, "${") && strings.HasSuffix(expr, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(expr, "${"), "}")
		return os.Getenv(envVar), nil
	}

	// Handle length calculation
	if strings.HasPrefix(expr, "len(") && strings.HasSuffix(expr, ")") {
		field := strings.TrimSuffix(strings.TrimPrefix(expr, "len("), ")")
		if value, ok := data[field]; ok {
			if arr, ok := value.([]interface{}); ok {
				return float64(len(arr)), nil
			}
		}
		return 0, nil
	}

	// Handle array access
	if strings.Contains(expr, "[") && strings.Contains(expr, "]") {
		parts := strings.Split(expr, "[")
		field := parts[0]
		if value, ok := data[field]; ok {
			if arr, ok := value.([]interface{}); ok {
				index := strings.TrimSuffix(parts[1], "]")
				if index == "*" {
					return arr, nil
				}
				if i, err := strconv.Atoi(index); err == nil && i < len(arr) {
					return arr[i], nil
				}
			}
		}
	}

	// Handle direct field access
	if value, ok := data[expr]; ok {
		return value, nil
	}

	// If the expression doesn't match any special patterns and isn't a field name,
	// treat it as a literal value
	return expr, nil
}

// evaluateCondition evaluates a condition expression
func (t *Transformer) evaluateCondition(expr string, data map[string]interface{}) (bool, error) {
	// Handle equality comparison
	if strings.Contains(expr, "==") {
		parts := strings.Split(expr, "==")
		if len(parts) != 2 {
			return false, fmt.Errorf("invalid equality expression: %s", expr)
		}

		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])

		leftValue, err := t.evaluateExpression(left, data)
		if err != nil {
			return false, err
		}

		// If right side is a literal string
		if strings.HasPrefix(right, "'") && strings.HasSuffix(right, "'") {
			right = strings.Trim(right, "'")
			return leftValue == right, nil
		}

		rightValue, err := t.evaluateExpression(right, data)
		if err != nil {
			return false, err
		}

		return leftValue == rightValue, nil
	}

	return false, fmt.Errorf("unsupported condition: %s", expr)
}

// Metrics recording functions
func (t *Transformer) recordSuccess(pipeline, step string) {
	if !t.config.Options.EnableMetrics {
		return
	}
	t.metrics.mu.Lock()
	defer t.metrics.mu.Unlock()
	t.metrics.transformCount++
	t.metrics.pipelineMetrics[pipeline]++
	t.metrics.successfulSteps[step]++
}

func (t *Transformer) recordError(pipeline, step string, err error) {
	if !t.config.Options.EnableMetrics {
		return
	}
	t.metrics.mu.Lock()
	defer t.metrics.mu.Unlock()
	t.metrics.errorCount++
	t.metrics.failedSteps[step]++
	t.metrics.lastError = err
}

// GetMetrics returns current transformation metrics
func (t *Transformer) GetMetrics() map[string]interface{} {
	if !t.config.Options.EnableMetrics {
		return nil
	}
	t.metrics.mu.RLock()
	defer t.metrics.mu.RUnlock()

	return map[string]interface{}{
		"transformCount":   t.metrics.transformCount,
		"errorCount":      t.metrics.errorCount,
		"pipelineMetrics": t.metrics.pipelineMetrics,
		"successfulSteps": t.metrics.successfulSteps,
		"failedSteps":     t.metrics.failedSteps,
		"lastError":       t.metrics.lastError,
	}
}
