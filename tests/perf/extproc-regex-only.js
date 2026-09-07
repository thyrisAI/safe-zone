import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const baseURL = __ENV.TSZ_PERF_BASE_URL || 'http://127.0.0.1:18080';
const rate = Number(__ENV.TSZ_PERF_RATE || 25);
const duration = __ENV.TSZ_PERF_DURATION || '2m';
const preAllocatedVUs = Number(__ENV.TSZ_PERF_PRE_ALLOCATED_VUS || 10);
const maxVUs = Number(__ENV.TSZ_PERF_MAX_VUS || 50);

const successfulResponses = new Counter('tsz_perf_successful_responses');
const requestDuration = new Trend('tsz_perf_request_duration', true);

export const options = {
  scenarios: {
    regex_only_request_path: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
  },
};

const payload = JSON.stringify({
  model: 'mock-openai',
  messages: [
    {
      role: 'user',
      content: 'Summarize the account-security controls in one sentence.',
    },
  ],
  stream: false,
});

export default function () {
  const response = http.post(`${baseURL}/v1/chat/completions`, payload, {
    headers: { 'Content-Type': 'application/json' },
    tags: { scenario: 'regex_only_request_path' },
  });

  requestDuration.add(response.timings.duration);
  const succeeded = check(response, {
    'returns HTTP 200': (result) => result.status === 200,
    'uses the mock OpenAI upstream': (result) => result.body.includes('chatcmpl-kind-mock'),
  });
  if (succeeded) {
    successfulResponses.add(1);
  }

  sleep(0.01);
}
