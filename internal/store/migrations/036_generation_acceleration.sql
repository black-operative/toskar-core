-- The backend and device that produced each reply (#317): vulkan and
-- "AMD Radeon RX 7900 XTX", or cpu and "". NULL for replies recorded
-- before, or that ran on a paired computer.
ALTER TABLE generation_metrics ADD COLUMN backend TEXT;
ALTER TABLE generation_metrics ADD COLUMN device TEXT;
