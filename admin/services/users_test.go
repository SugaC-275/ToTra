package services_test

import (
	"testing"

	"github.com/yourorg/totra/admin/services"
)

func TestCreateUserRequest_JobRole(t *testing.T) {
	req := services.CreateUserRequest{Name: "Bob", Email: "bob@acme.com", Role: "standard", JobRole: "engineer", Department: "platform"}
	if req.JobRole != "engineer" {
		t.Error("JobRole not set")
	}
	if req.Department != "platform" {
		t.Error("Department not set")
	}
}
