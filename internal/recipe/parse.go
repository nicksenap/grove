package recipe

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

func Parse(data []byte) Result {
	if len(data) > MaxRecipeBytes {
		return failure("file_too_large", "", 0, 0, fmt.Sprintf("Recipe exceeds %d bytes", MaxRecipeBytes))
	}

	doc, result := parseDocument(data)
	if len(result.Errors) > 0 {
		return result
	}

	if err := validateYAMLComplexity(doc, 0, new(int)); err != nil {
		return Result{Errors: []ValidationError{*err}}
	}

	root := documentRoot(doc)
	locations := make(map[string]location)
	collectLocations(root, "", locations)

	if err := rejectUnsupportedYAML(root, ""); err != nil {
		return Result{Errors: []ValidationError{*err}}
	}
	if errors := validateDuplicateKeys(root, ""); len(errors) > 0 {
		return Result{Errors: errors}
	}
	if errors := validateKnownFields(root, locations); len(errors) > 0 {
		return Result{Errors: errors}
	}
	if errors := validateSchemaTypes(root); len(errors) > 0 {
		return Result{Errors: errors}
	}

	var parsed Recipe
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&parsed); err != nil {
		code := "invalid_yaml"
		if isDuplicateKeyError(err) {
			code = "duplicate_key"
		}
		line, column := yamlErrorLocation(err)
		return failure(code, pathAtLine(locations, line), line, column, err.Error())
	}

	errors := validateRecipe(&parsed, locations)
	if len(errors) > 0 {
		return Result{Errors: errors}
	}
	return Result{Recipe: &parsed, Errors: []ValidationError{}}
}

func parseDocument(data []byte) (*yaml.Node, Result) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		if err == io.EOF {
			return nil, failure("invalid_yaml", "", 0, 0, "Recipe is empty")
		}
		code := "invalid_yaml"
		if isDuplicateKeyError(err) {
			code = "duplicate_key"
		}
		line, column := yamlErrorLocation(err)
		return nil, failure(code, "", line, column, err.Error())
	}

	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return nil, failure("multiple_documents", "", extra.Line, extra.Column, "Recipe must contain exactly one YAML document")
	} else if err != io.EOF {
		line, column := yamlErrorLocation(err)
		return nil, failure("invalid_yaml", "", line, column, err.Error())
	}
	return &doc, Result{}
}

func isDuplicateKeyError(err error) bool {
	message := err.Error()
	return strings.Contains(message, "mapping key") && strings.Contains(message, "already defined")
}

func yamlErrorLocation(err error) (int, int) {
	message := err.Error()
	marker := "line "
	index := strings.Index(message, marker)
	if index < 0 {
		return 0, 0
	}
	start := index + len(marker)
	end := start
	for end < len(message) && message[end] >= '0' && message[end] <= '9' {
		end++
	}
	line, parseErr := strconv.Atoi(message[start:end])
	if parseErr != nil {
		return 0, 0
	}
	return line, 1
}

func validateYAMLComplexity(node *yaml.Node, depth int, count *int) *ValidationError {
	if node == nil {
		return nil
	}
	*count++
	if depth > maxYAMLDepth || *count > maxYAMLNodes {
		return &ValidationError{Code: "yaml_too_complex", Line: node.Line, Column: node.Column, Message: "YAML structure exceeds Recipe complexity limits"}
	}
	for _, child := range node.Content {
		if err := validateYAMLComplexity(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

func rejectUnsupportedYAML(node *yaml.Node, nodePath string) *ValidationError {
	if node == nil {
		return nil
	}
	if node.Anchor != "" || node.Kind == yaml.AliasNode {
		return &ValidationError{Code: "unsupported_yaml", Path: nodePath, Line: node.Line, Column: node.Column, Message: "YAML anchors and aliases are not supported"}
	}
	if !allowedYAMLTag(node.Tag) {
		return &ValidationError{Code: "unsupported_yaml", Path: nodePath, Line: node.Line, Column: node.Column, Message: "custom YAML tags are not supported"}
	}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			childPath := joinPath(nodePath, key.Value)
			if err := rejectUnsupportedYAML(key, childPath); err != nil {
				return err
			}
			if err := rejectUnsupportedYAML(value, childPath); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for index, child := range node.Content {
			if err := rejectUnsupportedYAML(child, fmt.Sprintf("%s[%d]", nodePath, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func allowedYAMLTag(tag string) bool {
	switch tag {
	case "", "!!map", "!!seq", "!!str", "!!int", "!!bool", "!!null", "!!float":
		return true
	default:
		return false
	}
}

func validateDuplicateKeys(node *yaml.Node, nodePath string) []ValidationError {
	if node == nil {
		return nil
	}
	var errors []ValidationError
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]struct{}, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			childPath := joinPath(nodePath, key.Value)
			if key.Kind == yaml.ScalarNode {
				if _, duplicate := seen[key.Value]; duplicate {
					errors = append(errors, ValidationError{Code: "duplicate_key", Path: childPath, Line: key.Line, Column: key.Column, Message: "duplicate mapping key: " + key.Value})
				}
				seen[key.Value] = struct{}{}
			}
			errors = append(errors, validateDuplicateKeys(value, childPath)...)
		}
	} else if node.Kind == yaml.SequenceNode {
		for index, child := range node.Content {
			errors = append(errors, validateDuplicateKeys(child, fmt.Sprintf("%s[%d]", nodePath, index))...)
		}
	}
	return errors
}

func validateSchemaTypes(root *yaml.Node) []ValidationError {
	var errors []ValidationError
	if !requireNodeType(root, "", yaml.MappingNode, "mapping", &errors) {
		return errors
	}

	validateScalarField(root, "version", "", "!!int", "integer", &errors)
	validateScalarField(root, "name", "", "!!str", "string", &errors)

	repositories := mappingValue(root, "repositories")
	if repositories != nil && requireNodeType(repositories, "repositories", yaml.MappingNode, "mapping", &errors) {
		for i := 0; i+1 < len(repositories.Content); i += 2 {
			id, repository := repositories.Content[i], repositories.Content[i+1]
			basePath := "repositories." + id.Value
			requireStringKey(id, basePath, &errors)
			if requireNodeType(repository, basePath, yaml.MappingNode, "mapping", &errors) {
				validateScalarField(repository, "url", basePath, "!!str", "string", &errors)
				validateScalarField(repository, "ref", basePath, "!!str", "string", &errors)
			}
		}
	}

	jobs := mappingValue(root, "jobs")
	if jobs != nil && requireNodeType(jobs, "jobs", yaml.MappingNode, "mapping", &errors) {
		for i := 0; i+1 < len(jobs.Content); i += 2 {
			id, job := jobs.Content[i], jobs.Content[i+1]
			basePath := "jobs." + id.Value
			requireStringKey(id, basePath, &errors)
			validateJobTypes(job, basePath, &errors)
		}
	}
	return errors
}

func validateJobTypes(job *yaml.Node, basePath string, errors *[]ValidationError) {
	if !requireNodeType(job, basePath, yaml.MappingNode, "mapping", errors) {
		return
	}
	validateScalarField(job, "repository", basePath, "!!str", "string", errors)
	validateScalarField(job, "working-directory", basePath, "!!str", "string", errors)
	validateScalarField(job, "timeout-minutes", basePath, "!!int", "integer", errors)

	needs := mappingValue(job, "needs")
	if needs != nil && requireNodeType(needs, basePath+".needs", yaml.SequenceNode, "sequence", errors) {
		for index, dependency := range needs.Content {
			requireScalarTag(dependency, fmt.Sprintf("%s.needs[%d]", basePath, index), "!!str", "string", errors)
		}
	}
	steps := mappingValue(job, "steps")
	if steps != nil && requireNodeType(steps, basePath+".steps", yaml.SequenceNode, "sequence", errors) {
		for index, step := range steps.Content {
			stepPath := fmt.Sprintf("%s.steps[%d]", basePath, index)
			if requireNodeType(step, stepPath, yaml.MappingNode, "mapping", errors) {
				validateScalarField(step, "name", stepPath, "!!str", "string", errors)
				validateScalarField(step, "run", stepPath, "!!str", "string", errors)
			}
		}
	}
}

func validateScalarField(mapping *yaml.Node, field, parentPath, tag, description string, errors *[]ValidationError) {
	if node := mappingValue(mapping, field); node != nil {
		requireScalarTag(node, joinPath(parentPath, field), tag, description, errors)
	}
}

func requireStringKey(node *yaml.Node, valuePath string, errors *[]ValidationError) {
	requireScalarTag(node, valuePath, "!!str", "string key", errors)
}

func requireScalarTag(node *yaml.Node, valuePath, tag, description string, errors *[]ValidationError) bool {
	if !requireNodeType(node, valuePath, yaml.ScalarNode, description, errors) {
		return false
	}
	if node.Tag != tag {
		*errors = append(*errors, typeError(node, valuePath, description))
		return false
	}
	return true
}

func requireNodeType(node *yaml.Node, valuePath string, kind yaml.Kind, description string, errors *[]ValidationError) bool {
	if node != nil && node.Kind == kind && node.Tag != "!!null" {
		return true
	}
	*errors = append(*errors, typeError(node, valuePath, description))
	return false
}

func typeError(node *yaml.Node, valuePath, expected string) ValidationError {
	err := ValidationError{Code: "invalid_type", Path: valuePath, Message: "expected " + expected}
	if node != nil {
		err.Line = node.Line
		err.Column = node.Column
	}
	return err
}

func validateKnownFields(root *yaml.Node, locations map[string]location) []ValidationError {
	var errors []ValidationError
	checkAllowedFields(root, "", set("version", "name", "repositories", "jobs"), &errors)

	if repositories := mappingValue(root, "repositories"); repositories != nil && repositories.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(repositories.Content); i += 2 {
			id := repositories.Content[i].Value
			checkAllowedFields(repositories.Content[i+1], "repositories."+id, set("url", "ref"), &errors)
		}
	}
	if jobs := mappingValue(root, "jobs"); jobs != nil && jobs.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(jobs.Content); i += 2 {
			id := jobs.Content[i].Value
			jobPath := "jobs." + id
			jobNode := jobs.Content[i+1]
			checkAllowedFields(jobNode, jobPath, set("repository", "working-directory", "timeout-minutes", "needs", "steps"), &errors)
			if steps := mappingValue(jobNode, "steps"); steps != nil && steps.Kind == yaml.SequenceNode {
				for index, step := range steps.Content {
					checkAllowedFields(step, fmt.Sprintf("%s.steps[%d]", jobPath, index), set("name", "run"), &errors)
				}
			}
		}
	}
	return errors
}

func checkAllowedFields(node *yaml.Node, nodePath string, allowed map[string]struct{}, errors *[]ValidationError) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		if _, ok := allowed[key.Value]; ok {
			continue
		}
		fieldPath := joinPath(nodePath, key.Value)
		*errors = append(*errors, ValidationError{Code: "unknown_field", Path: fieldPath, Line: key.Line, Column: key.Column, Message: "unknown field: " + key.Value})
	}
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc != nil && doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func set(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func failure(code, valuePath string, line, column int, message string) Result {
	return Result{Errors: []ValidationError{{Code: code, Path: valuePath, Line: line, Column: column, Message: message}}}
}
