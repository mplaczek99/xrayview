const jobSubmitTimes = new Map<string, number>();

export function recordJobSubmit(jobId: string) {
  jobSubmitTimes.set(jobId, performance.now());
}

export function logCompletedJobVisibleTiming(jobId: string) {
  const submittedAt = jobSubmitTimes.get(jobId);
  if (submittedAt === undefined) {
    return;
  }

  console.info(`[bench] ${jobId} visible in ${performance.now() - submittedAt}ms`);
  jobSubmitTimes.delete(jobId);
}

export function clearJobSubmitTiming(jobId: string) {
  jobSubmitTimes.delete(jobId);
}
