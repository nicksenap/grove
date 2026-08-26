package e2e

import (
	"path/filepath"
	"testing"
)

func TestRecipeCreate(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.createRepo("svc-api")
	env.addGroveTOML("svc-auth", "setup = \"touch .grove-setup-ran\"\n")
	env.init()

	authOrigin := filepath.Join(env.home, "recipe-auth.git")
	apiOrigin := filepath.Join(env.home, "recipe-api.git")
	env.git(env.home, "clone", "-q", "--bare", filepath.Join(env.reposDir, "svc-auth"), authOrigin)
	env.git(env.home, "clone", "-q", "--bare", filepath.Join(env.reposDir, "svc-api"), apiOrigin)
	env.git(filepath.Join(env.reposDir, "svc-auth"), "remote", "add", "origin", fileURL(authOrigin))
	env.git(filepath.Join(env.reposDir, "svc-api"), "remote", "add", "origin", fileURL(apiOrigin))
	env.git(filepath.Join(env.reposDir, "svc-auth"), "fetch", "-q", "origin")
	env.git(filepath.Join(env.reposDir, "svc-api"), "fetch", "-q", "origin")

	recipeFile := filepath.Join(env.home, "recipe.yaml")
	env.writeFile(recipeFile, `version: 1
name: e2e-recipe
repositories:
  auth:
    url: `+fileURL(authOrigin)+`
    ref: main
  api:
    url: `+fileURL(apiOrigin)+`
    ref: main
jobs:
  setup-auth:
    repository: auth
    steps:
      - run: touch .recipe-ran
  setup-api:
    repository: api
    steps:
      - run: touch .recipe-ran
  verify:
    repository: auth
    needs: [setup-auth, setup-api]
    steps:
      - run: test -f .recipe-ran && test -f ../api/.recipe-ran
`)

	res := env.mustGW("create", "recipe-ws", "--branch", "feat/recipe-e2e", "--recipe", recipeFile, "--json")
	out := decodeJSON[recipeCreateJSON](t, res.stdout)
	if !out.Created || out.Name != "recipe-ws" || len(out.Jobs) != 3 {
		t.Fatalf("create --recipe JSON failed: %+v raw=%s", out, res.stdout)
	}
	env.requireExists(filepath.Join(env.worktree("recipe-ws", "auth"), ".recipe-ran"))
	env.requireExists(filepath.Join(env.worktree("recipe-ws", "api"), ".recipe-ran"))
	env.requireMissing(filepath.Join(env.worktree("recipe-ws", "auth"), ".grove-setup-ran"))
	env.mustGW("delete", "recipe-ws")

	failFile := filepath.Join(env.home, "recipe-fail.yaml")
	env.writeFile(failFile, `version: 1
repositories:
  api:
    url: `+fileURL(apiOrigin)+`
    ref: main
jobs:
  fail:
    repository: api
    steps:
      - name: Fail deliberately
        run: touch generated.txt; exit 7
`)
	failed := env.gw("create", "failed-recipe", "--branch", "feat/recipe-fail", "--recipe", failFile, "--json")
	failed.mustFail(t)
	decoded := decodeJSON[recipeCreateJSON](t, failed.stdout)
	if decoded.Created || decoded.Error == nil || decoded.Error.Code != "recipe_step_failed" || decoded.Error.Job != "fail" || decoded.Error.Step != 1 {
		t.Fatalf("Recipe failure JSON was not actionable: %+v raw=%s", decoded, failed.stdout)
	}
	env.requireMissing(env.workspacePath("failed-recipe"))
	if env.branchExists(filepath.Join(env.reposDir, "svc-api"), "feat/recipe-fail") {
		t.Fatal("failed Recipe rollback left workspace or branch")
	}

	env.gw("create", "conflict-recipe", "--branch", "feat/conflict", "--recipe", recipeFile, "--repos", "svc-api").mustFail(t)
}
