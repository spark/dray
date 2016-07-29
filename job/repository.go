package job

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"

	log "github.com/Sirupsen/logrus"
	"github.com/fzzy/radix/extra/pool"
	"github.com/fzzy/radix/redis"
	"github.com/CenturyLinkLabs/dray/util"
)

// NotFoundError is an error returned when a referenced Job cannot be found.
type NotFoundError string

// Error returns the error string for the NotFoundError
func (s NotFoundError) Error() string {
	return fmt.Sprintf("Cannot find job with ID %s", string(s))
}

type redisJobRepository struct {
	pool *pool.Pool
	auth string
}

// NewJobRepository returns a new JobRepository instance with a connection to
// the specified Redis endpoint.
func NewJobRepository(host string, auth string) JobRepository {
	pool, err := pool.NewPool("tcp", host, 4)
	if err != nil {
		log.Errorf("Error instantiating Redis pool: %s", err)
		panic(err)
	}

	return &redisJobRepository{pool: pool, auth: auth}
}

func (r *redisJobRepository) All() ([]Job, error) {
	jobs := []Job{}

	jobIDs, err := r.command("lrange", util.GetConfig().JobsKey, 0, -1).List()
	if err != nil {
		return nil, err
	}

	for _, jobID := range jobIDs {
		jobs = append(jobs, Job{ID: jobID})
	}

	return jobs, nil
}

func (r *redisJobRepository) Get(jobID string) (*Job, error) {
	job := Job{ID: jobID}
	reply := r.command("hgetall", jobKey(jobID))

	if len(reply.Elems) == 0 {
		return nil, NotFoundError(jobID)
	}

	status, err := reply.Hash()
	if err != nil {
		return nil, err
	}

	job.StepsCompleted, _ = strconv.Atoi(status["completedSteps"])
	job.Status = status["status"]
	job.CreatedAt = status["createdAt"]
	job.FinishedIn, _ = strconv.ParseFloat(status["finishedIn"], 64)
	return &job, nil
}

func (r *redisJobRepository) Create(job *Job) error {
	job.ID = pseudoUUID()

	reply := r.command("rpush", util.GetConfig().JobsKey, job.ID)
	if reply.Err != nil {
		return reply.Err
	}

	totalSteps := string(len(job.Steps))
	reply = r.command("hmset", jobKey(job.ID), "totalSteps", totalSteps, "completedSteps", "0", "status", "")

	if (util.GetConfig().KeyTTL > 0) {
		defer r.command("expire", jobKey(job.ID), util.GetConfig().KeyTTL)
	}

	return reply.Err
}

func (r *redisJobRepository) DeleteFromIndex(jobID string) error {
	reply :=  r.command("lrem", util.GetConfig().JobsKey, 0, jobID)
	return reply.Err
}

func (r *redisJobRepository) Delete(jobID string) error {
	errorReply := r.DeleteFromIndex(jobID)
	if errorReply != nil {
		return errorReply
	}

	reply := r.command("del", jobKey(jobID))
	if reply.Err != nil {
		return reply.Err
	}

	reply = r.command("del", jobLogKey(jobID))
	return reply.Err
}

func (r *redisJobRepository) Update(jobID, attr, value string) error {
	reply := r.command("hset", jobKey(jobID), attr, value)

	// Publish field update message
	defer r.command("publish", jobID + ":" + attr, value)

	return reply.Err
}

func (r *redisJobRepository) GetJobLog(jobID string, index int) (*JobLog, error) {
	lines, err := r.command("lrange", jobLogKey(jobID), index, -1).List()
	if err != nil {
		return nil, err
	}

	return &JobLog{Lines: lines}, nil
}

func (r *redisJobRepository) AppendLogLine(jobID, logLine string) error {
	reply := r.command("rpush", jobLogKey(jobID), logLine)

	if (util.GetConfig().KeyTTL > 0) {
		defer r.command("expire", jobLogKey(jobID), util.GetConfig().KeyTTL)
	}
	return reply.Err
}

func (r *redisJobRepository) command(cmd string, args ...interface{}) *redis.Reply {
	client, err := r.pool.Get()
	if err != nil {
		return &redis.Reply{Err: err}
	}
	defer r.pool.Put(client)

	// If authentication was provided, use if for this connection
	if r.auth != "" {
		authReply := client.Cmd("auth", r.auth)
		if authReply.Err != nil {
			if _, ok := authReply.Err.(*redis.CmdError); !ok {
				authReply.Err = errors.New("Redis authentication error")
			}
			return authReply
		}
	}

	reply := client.Cmd(cmd, args...)

	// Use a more friendly error message for connection problems
	if reply.Err != nil {
		if _, ok := reply.Err.(*redis.CmdError); !ok {
			reply.Err = errors.New("Redis connection error")
		}
	}

	return reply
}

func jobKey(jobID string) string {
	return fmt.Sprintf("%s:%s", util.GetConfig().JobsKey, jobID)
}

func jobLogKey(jobID string) string {
	return fmt.Sprintf("%s:%s:log", util.GetConfig().JobsKey, jobID)
}

func pseudoUUID() (uuid string) {
	b := make([]byte, 16)
	rand.Read(b)

	return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
