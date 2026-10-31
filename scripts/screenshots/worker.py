# Small CPU load so the meters and CPU% column have something to show.
import sys, time
duty = float(sys.argv[1]) if len(sys.argv) > 1 else 0.3
while True:
    end = time.time() + duty * 0.1
    while time.time() < end:
        pass
    time.sleep((1 - duty) * 0.1)
