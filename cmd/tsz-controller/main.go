// tsz-controller runs the TSZ Kubernetes control plane independently from the
// ext-proc data plane so its Kubernetes permissions remain narrowly scoped.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"thyris-sz/internal/cache"
	"thyris-sz/internal/config"
	controller "thyris-sz/internal/controller"
	"thyris-sz/internal/controller/effectivepolicy"
	"thyris-sz/internal/controller/envoyresource"
	"thyris-sz/internal/controller/policyattach"
	"thyris-sz/internal/database"
	"thyris-sz/internal/extproc/policy"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	leaderElectionID  = "tsz-controller.security.thyris.ai"
	dependencyTimeout = 2 * time.Second
)

func main() {
	config.LoadConfig()
	database.InitDB()
	cache.InitRedis()

	sqlDB, err := database.DB.DB()
	if err != nil {
		log.Fatalf("get PostgreSQL connection pool: %v", err)
	}
	defer sqlDB.Close()
	repository, err := policy.NewPostgresRepository(sqlDB)
	if err != nil {
		log.Fatalf("initialize policy repository: %v", err)
	}
	compiler, err := policy.NewCompiler(repository)
	if err != nil {
		log.Fatalf("initialize policy compiler: %v", err)
	}
	publisher, err := policy.NewRedisActivationPublisher(cache.RDB)
	if err != nil {
		log.Fatalf("initialize policy activation publisher: %v", err)
	}
	activator, err := policy.NewActivator(repository, publisher)
	if err != nil {
		log.Fatalf("initialize policy activator: %v", err)
	}
	ownership, err := policy.NewCRDOwnershipTracker(sqlDB)
	if err != nil {
		log.Fatalf("initialize Inline policy ownership tracker: %v", err)
	}

	scheme, err := controller.NewScheme()
	if err != nil {
		log.Fatalf("build controller scheme: %v", err)
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		LeaderElection:         true,
		LeaderElectionID:       leaderElectionID,
		HealthProbeBindAddress: ":8081",
		Metrics:                metricsserver.Options{BindAddress: ":8080"},
	})
	if err != nil {
		log.Fatalf("create controller manager: %v", err)
	}

	reconciler := policyattach.NewPolicyAttachmentReconciler(
		mgr.GetClient(),
		&policyattach.Resolver{Client: mgr.GetClient()},
		effectivepolicy.Selector{},
		&effectivepolicy.ReferenceResolver{Repo: repository},
		&effectivepolicy.Compiler{Repo: repository, Compiler: compiler, Activator: activator},
		&envoyresource.EnvoyResourceReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()},
		ownership,
	)
	if err := reconciler.SetupWithManager(mgr); err != nil {
		log.Fatalf("configure policy attachment controller: %v", err)
	}
	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		log.Fatalf("add liveness check: %v", err)
	}
	if err := mgr.AddReadyzCheck("cache-sync", cacheSyncReadiness(mgr.GetCache().WaitForCacheSync)); err != nil {
		log.Fatalf("add cache readiness check: %v", err)
	}
	if err := mgr.AddReadyzCheck("postgres", databaseReadiness(sqlDB)); err != nil {
		log.Fatalf("add PostgreSQL readiness check: %v", err)
	}
	if err := mgr.AddReadyzCheck("redis", redisReadiness(cache.RDB)); err != nil {
		log.Fatalf("add Redis readiness check: %v", err)
	}

	log.Printf("tsz-controller starting (leader election id=%s)", leaderElectionID)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Fatalf("run controller manager: %v", err)
	}
}

func cacheSyncReadiness(waitForCacheSync func(context.Context) bool) healthz.Checker {
	return func(request *http.Request) error {
		ctx, cancel := context.WithTimeout(request.Context(), dependencyTimeout)
		defer cancel()
		if !waitForCacheSync(ctx) {
			return fmt.Errorf("controller cache has not synchronized")
		}
		return nil
	}
}

func databaseReadiness(db *sql.DB) healthz.Checker {
	return func(request *http.Request) error {
		ctx, cancel := context.WithTimeout(request.Context(), dependencyTimeout)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			return fmt.Errorf("PostgreSQL unavailable: %w", err)
		}
		return nil
	}
}

func redisReadiness(rdb *redis.Client) healthz.Checker {
	return func(request *http.Request) error {
		if rdb == nil {
			return fmt.Errorf("Redis client is not initialized")
		}
		ctx, cancel := context.WithTimeout(request.Context(), dependencyTimeout)
		defer cancel()
		if err := rdb.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("Redis unavailable: %w", err)
		}
		return nil
	}
}
